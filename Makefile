.PHONY: proto

proto:
	protoc --go_out=. --go_opt=module=github.com/itchio/lake tlc/tlc.proto
