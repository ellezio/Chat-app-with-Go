.PHONY: all run templ_watch tailwind_watch

TAILWIND_INPUT_CSS := web/assets/css/tailwind.css
TAILWIND_OUTPUT_CSS := web/assets/css/output.css

MONGODB_URI ?= localhost:27017

all:
	$(MAKE) templ_watch &
	$(MAKE) tailwind_watch &
	$(MAKE) run

run:
	@MONGODB_URI=$(MONGODB_URI) go run ./cmd/webapp &
	@MONGODB_URI=$(MONGODB_URI) go run ./cmd/chat-server

templ_watch:
	@templ generate --watch

tailwind_watch:
	@tailwindcss -i $(TAILWIND_INPUT_CSS) -o $(TAILWIND_OUTPUT_CSS) --watch
