.PHONY: build test fmt vet check serve compile-fixtures clean e2e shots demo site site-check

build:
	go build -o bin/kavira ./cmd/kavira

test:
	go test -race ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

# Same gates as CI: gofmt, vet, race tests, build.
check:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }
	$(MAKE) vet test build

serve: build
	./bin/kavira serve

compile-fixtures: build
	./bin/kavira compile -f examples/oom.json -o repairs
	./bin/kavira compile -f examples/network-timeout.json -o repairs
	./bin/kavira compile -f examples/config-regression.json -o repairs

clean:
	rm -rf bin repairs coverage.out

# Browser end-to-end + screenshots. Needs playwright(-core): npm i -D playwright-core, or KAVIRA_PW_PATH=<node_modules dir>.
e2e: build
	node scripts/e2e.cjs

shots: build
	node scripts/e2e.cjs --shots docs/ux

demo: build
	node scripts/demo.cjs

# The Pages site: landing + live demo (the real console on recorded API responses), assembled in _site/.
site:
	./scripts/build-site.sh

site-check: site
	node scripts/site-check.cjs
