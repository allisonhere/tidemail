package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"time"
)

func main() {
	// Create a small 80x40 red test image (10 cols x 2.5 rows at 8x16 cell size)
	img := image.NewRGBA(image.Rect(0, 0, 80, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 80; x++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}

	// Encode as PNG
	var pngBuf bytes.Buffer
	png.Encode(&pngBuf, img)
	pngData := base64.StdEncoding.EncodeToString(pngBuf.Bytes())

	// Also prepare raw RGBA
	rgbaData := base64.StdEncoding.EncodeToString(img.Pix)

	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer tty.Close()

	fmt.Println("=== Kitty Graphics Test ===")
	fmt.Println()
	fmt.Printf("TERM_PROGRAM=%s\n", os.Getenv("TERM_PROGRAM"))
	fmt.Printf("TERM=%s\n", os.Getenv("TERM"))
	fmt.Println()

	// Test 1: a=T with PNG format (f=100), inline data
	fmt.Println("Test 1: a=T, f=100 (PNG), t=d (inline)")
	fmt.Println("  (should show red box below)")
	time.Sleep(200 * time.Millisecond)
	fmt.Fprintf(tty, "\x1b_Gf=100,a=T,t=d;%s\x1b\\", pngData)
	fmt.Println()
	fmt.Println()
	fmt.Println()
	time.Sleep(1 * time.Second)

	// Test 2: a=T with RGBA format (f=32), inline data
	fmt.Println("Test 2: a=T, f=32 (RGBA), s=80,v=40, t=d (inline)")
	fmt.Println("  (should show red box below)")
	time.Sleep(200 * time.Millisecond)
	fmt.Fprintf(tty, "\x1b_Gf=32,s=80,v=40,a=T,t=d;%s\x1b\\", rgbaData)
	fmt.Println()
	fmt.Println()
	fmt.Println()
	time.Sleep(1 * time.Second)

	// Test 3: a=T with cursor positioning
	fmt.Println("Test 3: a=T with cursor save/move/restore")
	fmt.Println("  (should show red box on next line)")
	fmt.Println()
	time.Sleep(200 * time.Millisecond)
	// Get current cursor position roughly — place at row 20, col 5
	fmt.Fprintf(tty, "\x1b7\x1b_Gf=100,a=T,t=d;%s\x1b\\\x1b8", pngData)
	fmt.Println()
	fmt.Println()
	fmt.Println()
	time.Sleep(1 * time.Second)

	// Test 4: a=t then a=p (two-step)
	fmt.Println("Test 4: a=t (transmit) then a=p (place), i=99")
	fmt.Println("  (should show red box below)")
	time.Sleep(200 * time.Millisecond)
	fmt.Fprintf(tty, "\x1b_Gf=100,i=99,a=t,t=d;%s\x1b\\", pngData)
	time.Sleep(100 * time.Millisecond)
	fmt.Fprintf(tty, "\x1b_Ga=p,i=99\x1b\\")
	fmt.Println()
	fmt.Println()
	fmt.Println()
	time.Sleep(1 * time.Second)

	// Test 5: a=T with BEL terminator instead of ST
	fmt.Println("Test 5: a=T, f=100 (PNG), BEL terminator")
	fmt.Println("  (should show red box below)")
	time.Sleep(200 * time.Millisecond)
	fmt.Fprintf(tty, "\x1b_Gf=100,a=T,t=d;%s\x07", pngData)
	fmt.Println()
	fmt.Println()
	fmt.Println()
	time.Sleep(1 * time.Second)

	// Test 6: Write to stdout instead of /dev/tty
	fmt.Println("Test 6: a=T, f=100 (PNG), writing to STDOUT")
	fmt.Println("  (should show red box below)")
	time.Sleep(200 * time.Millisecond)
	fmt.Fprintf(os.Stdout, "\x1b_Gf=100,a=T,t=d;%s\x1b\\", pngData)
	fmt.Println()
	fmt.Println()
	fmt.Println()
	time.Sleep(1 * time.Second)

	fmt.Println("=== Done ===")
	fmt.Println("Which tests showed a red box?")
}
