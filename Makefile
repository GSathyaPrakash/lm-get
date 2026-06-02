PREFIX ?= /usr/local
BINARY = lm-get

.PHONY: build install clean

build:
	go build -o $(BINARY) .

install: build
	install -d $(DESTDIR)$(PREFIX)/bin
	install -m 755 $(BINARY) $(DESTDIR)$(PREFIX)/bin/$(BINARY)

uninstall:
	rm -f $(DESTDIR)$(PREFIX)/bin/$(BINARY)

clean:
	rm -f $(BINARY)

strip: build
	strip $(BINARY)
