package main

import _ "embed"

var (
	//go:embed chat.html
	chatHTML string
	//go:embed chat.css
	chatCSS string
	//go:embed chat.js
	chatJS string
)
