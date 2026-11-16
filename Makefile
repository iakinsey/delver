default: ui
	go build .

ui:
	npm --prefix web run build
	node scripts/package-ui.mjs
