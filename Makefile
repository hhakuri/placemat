# Run github.com/rakyll/statik by `go generate`

STATIK_FILES := $(wildcard cmd/placemat-menu/public/*/*)
STATIK_TARGET := cmd/placemat-menu/statik/statik.go

all: $(STATIK_TARGET)
	go install ./...

$(STATIK_TARGET): $(STATIK_FILES)
	go get github.com/rakyll/statik/...
	go generate ./...

.PHONY:	all
