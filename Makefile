.PHONY: run

DEV_API_ADDR ?= 127.0.0.1:8001

run:
	@set -eu; \
	mock_bin=$$(mktemp); \
	mock_pid=; \
	cleanup() { \
		if test -n "$$mock_pid"; then \
			kill "$$mock_pid" 2>/dev/null || true; \
			wait "$$mock_pid" 2>/dev/null || true; \
		fi; \
		rm -f "$$mock_bin"; \
	}; \
	trap cleanup EXIT; \
	trap 'exit 130' INT; \
	trap 'exit 143' TERM; \
	go build -o "$$mock_bin" ./cmd/devmock; \
	"$$mock_bin" -addr "$(DEV_API_ADDR)" & \
	mock_pid=$$!; \
	BAENDAELI_URL="http://$(DEV_API_ADDR)" go run .
