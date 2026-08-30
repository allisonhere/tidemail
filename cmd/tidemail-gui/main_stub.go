//go:build !desktop

package main

import "fmt"

func main() {
	fmt.Println("tidemail-gui is a desktop build; run `wails dev` in cmd/tidemail-gui")
}
