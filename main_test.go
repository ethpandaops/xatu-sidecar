package main

import (
	"errors"
	"os"
	"reflect"
	"testing"
)

const awkwardValue = "a\tb #c\nd: e\"f'g\\h"

func TestDecodeConfig(t *testing.T) {
	tests := []struct {
		name    string
		content string
		set     map[string]string
		unset   []string
		want    map[string]any
		wantErr string
	}{
		{
			name:    "variable is replaced",
			content: "authorization: Basic ${XATU_AUTH}",
			set:     map[string]string{"XATU_AUTH": "s3cret"},
			want:    map[string]any{"authorization": "Basic s3cret"},
		},
		{
			name:    "multiple variables in one value",
			content: "addr: ${HOST}:${PORT}",
			set:     map[string]string{"HOST": "example.com", "PORT": "8080"},
			want:    map[string]any{"addr": "example.com:8080"},
		},
		{
			name:    "default used when variable unset",
			content: "addr: ${HOST:-localhost}",
			unset:   []string{"HOST"},
			want:    map[string]any{"addr": "localhost"},
		},
		{
			name:    "default used when variable empty",
			content: "addr: ${HOST:-localhost}",
			set:     map[string]string{"HOST": ""},
			want:    map[string]any{"addr": "localhost"},
		},
		{
			name:    "default ignored when variable set",
			content: "addr: ${HOST:-localhost}",
			set:     map[string]string{"HOST": "example.com"},
			want:    map[string]any{"addr": "example.com"},
		},
		{
			name:    "empty default allows unset variable",
			content: "token: \"${TOKEN:-}\"",
			unset:   []string{"TOKEN"},
			want:    map[string]any{"token": ""},
		},
		{
			name:    "empty variable without default is an error",
			content: "token: ${TOKEN}",
			set:     map[string]string{"TOKEN": ""},
			wantErr: "config references unset or empty environment variables: TOKEN",
		},
		{
			name:    "unset variables without default are an error",
			content: "user: ${XATU_USER}\nnested:\n  - pass: ${XATU_PASS}\nagain: ${XATU_USER}",
			unset:   []string{"XATU_USER", "XATU_PASS"},
			wantErr: "config references unset or empty environment variables: XATU_USER, XATU_PASS",
		},
		{
			name:    "comments are not expanded",
			content: "# auth: ${XATU_PASS}\naddr: ${HOST} # ${XATU_PASS}",
			set:     map[string]string{"HOST": "example.com"},
			unset:   []string{"XATU_PASS"},
			want:    map[string]any{"addr": "example.com"},
		},
		{
			name:    "keys are not expanded",
			content: "${HOST}: value",
			set:     map[string]string{"HOST": "example.com"},
			want:    map[string]any{"${HOST}": "value"},
		},
		{
			name:    "literal dollar signs are left unchanged",
			content: "password: pa$$word $HOST $5 ${HOST",
			set:     map[string]string{"HOST": "example.com"},
			want:    map[string]any{"password": "pa$$word $HOST $5 ${HOST"},
		},
		{
			name:    "config without placeholders is unchanged",
			content: "name: test # comment\nport: 8080\ntls: true\nlist:\n  - a\n",
			want:    map[string]any{"name": "test", "port": 8080, "tls": true, "list": []any{"a"}},
		},
		{
			name:    "value with YAML syntax arrives unchanged",
			content: "plain: ${P}\ndouble: \"${P}\"\nsingle: '${P}'\nlist:\n  - ${P}\nanchored: &a ${P}\naliased: *a",
			set:     map[string]string{"P": awkwardValue},
			want: map[string]any{
				"plain":    awkwardValue,
				"double":   awkwardValue,
				"single":   awkwardValue,
				"list":     []any{awkwardValue},
				"anchored": awkwardValue,
				"aliased":  awkwardValue,
			},
		},
		{
			name:    "unquoted value resolves its type, quoted value stays a string",
			content: "port: ${PORT}\nquoted: \"${PORT}\"\nenabled: ${ENABLED}",
			set:     map[string]string{"PORT": "8080", "ENABLED": "true"},
			want:    map[string]any{"port": 8080, "quoted": "8080", "enabled": true},
		},
		{
			name:    "unquoted value that resolves to null stays a string",
			content: "a: ${NULL_WORD}\nb: ${NULL_TILDE}\nc: ${NULL_UNSET:-}",
			set:     map[string]string{"NULL_WORD": "null", "NULL_TILDE": "~"},
			unset:   []string{"NULL_UNSET"},
			want:    map[string]any{"a": "null", "b": "~", "c": ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.set {
				t.Setenv(k, v)
			}

			for _, k := range tt.unset {
				t.Setenv(k, "")

				if err := os.Unsetenv(k); err != nil {
					t.Fatal(err)
				}
			}

			var got map[string]any

			err := decodeConfig([]byte(tt.content), &got)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr || !errors.Is(err, errUnsetEnvVar) {
					t.Fatalf("got error %v, want %q", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestDecodeConfigIntoConfig(t *testing.T) {
	t.Setenv("XATU_SIDECAR_TEST_NAME", awkwardValue)
	t.Setenv("XATU_SIDECAR_TEST_GENESIS", "1606824023")

	var config *Config

	err := decodeConfig([]byte("log_level: ${XATU_SIDECAR_TEST_LEVEL:-debug}\nprocessor:\n  name: ${XATU_SIDECAR_TEST_NAME}\n  ethereum:\n    genesis_time: ${XATU_SIDECAR_TEST_GENESIS}\n"), &config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if *config.LogLevel != "debug" || config.Processor.Name != awkwardValue || config.Processor.Ethereum.GenesisTime != 1606824023 {
		t.Fatalf("got log_level %q, name %q and genesis_time %d", *config.LogLevel, config.Processor.Name, config.Processor.Ethereum.GenesisTime)
	}
}
