build:
	go mod tidy
	go build -buildmode=c-shared -o libxatu.so .

clean:
	rm -f libxatu.so libxatu.h