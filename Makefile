.PHONY: all run templ_watch tailwind_watch

TAILWIND_INPUT_CSS := web/assets/css/tailwind.css
TAILWIND_OUTPUT_CSS := web/assets/css/output.css

MONGODB_URI ?= localhost:27017

all:
	@trap 'kill 0' EXIT; \
	$(MAKE) templ_watch & \
	$(MAKE) tailwind_watch & \
	$(MAKE) run & \
	wait

run:
	@trap 'kill 0' EXIT; \
	go run ./cmd/file-server --dir ./web/files & \
	MONGODB_URI=$(MONGODB_URI) go run ./cmd/chat-server & \
	MONGODB_URI=$(MONGODB_URI) go run ./cmd/webapp & \
	wait

templ_watch:
	templ generate --watch

tailwind_watch:
	tailwindcss -i $(TAILWIND_INPUT_CSS) -o $(TAILWIND_OUTPUT_CSS) --watch
