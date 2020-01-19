ARCHITECTURE=armhf
PACKAGE_NAME=sarah
PACKAGE_VERSION=1.0.0
PACKAGE_DESCRIPTION="Simple Sane Rest Api"

clean:
	rm -vrf ./build
	rm -vf sarah

build: 
	go build sarah.go

deb: clean build
	mkdir -p ./build/usr/local/bin
	mkdir -p ./build/etc/systemd/system
	cp sarah ./build/usr/local/bin
	cp sarah.service ./build/etc/systemd/system
	touch ./build/etc/sarahrc
	chmod 666 ./build/etc/sarahrc

	fpm -s dir -t deb -v $(PACKAGE_VERSION)  -n $(PACKAGE_NAME) -a $(ARCHITECTURE) --description $(PACKAGE_DESCRIPTION) -C ./build