TAILWIND_INPUT_CSS := web/assets/css/tailwind.css
TAILWIND_OUTPUT_CSS := web/assets/css/output.css

.PHONY: all run templ_watch tailwind_watch

all:
	$(MAKE) templ_watch &
	$(MAKE) tailwind_watch &
	$(MAKE) run

run:
	@go run ./cmd/webapp

templ_watch:
	@templ generate --watch

tailwind_watch:
	@tailwindcss -i $(TAILWIND_INPUT_CSS) -o $(TAILWIND_OUTPUT_CSS) --watch
