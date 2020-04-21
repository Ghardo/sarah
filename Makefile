ARCHITECTURE=armhf
PACKAGE_NAME=sarah
PACKAGE_VERSION=1.0.1
PACKAGE_DESCRIPTION="Simple Sane Rest Api"

clean:
	rm -vrf ./build
	rm -vf sarah

build: clean
	go build sarah.go

deb: clean build
	mkdir -pv ./build/usr/local/bin
	mkdir -pv ./build/etc/systemd/system
	cp -v sarah ./build/usr/local/bin
	cp -v sarah.service ./build/etc/systemd/system
	touch ./build/etc/sarahrc
	chmod -v 666 ./build/etc/sarahrc

	fpm -s dir -t deb -v $(PACKAGE_VERSION)  -n $(PACKAGE_NAME) -a $(ARCHITECTURE) --description $(PACKAGE_DESCRIPTION) -C ./build