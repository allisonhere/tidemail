package imap

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/allisonhere/tidemail/internal/config"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCopyParseFailureRecoversWithoutRepeatingCopy(t *testing.T) {
	for _, validity := range []int{1, 2} {
		t.Run(fmt.Sprint(validity), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			var copies, stores atomic.Int32
			go func() {
				for attempt := 0; attempt < 2; attempt++ {
					conn, err := listener.Accept()
					if err != nil {
						return
					}
					func() {
						defer conn.Close()
						_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
						write := func(s string) { _, _ = conn.Write([]byte(s)) }
						write("* OK [CAPABILITY IMAP4rev1] ready\r\n")
						reader := bufio.NewScanner(conn)
						for reader.Scan() {
							fields := strings.Fields(reader.Text())
							if len(fields) < 2 {
								continue
							}
							tag, cmd := fields[0], strings.ToUpper(fields[1])
							switch cmd {
							case "CAPABILITY":
								write("* CAPABILITY IMAP4rev1\r\n" + tag + " OK done\r\n")
							case "LOGIN":
								write(tag + " OK done\r\n")
							case "SELECT":
								v := 1
								if attempt == 1 {
									v = validity
								}
								write(fmt.Sprintf("* 1 EXISTS\r\n* OK [UIDVALIDITY %d] valid\r\n%s OK [READ-WRITE] selected\r\n", v, tag))
							case "UID":
								switch strings.ToUpper(fields[2]) {
								case "COPY":
									copies.Add(1)
									write(tag + " OK [COPYUID 1 1 0] copied\r\n")
									return
								case "STORE":
									stores.Add(1)
									write(tag + " OK stored\r\n")
								case "SEARCH":
									write("* SEARCH 1\r\n" + tag + " OK searched\r\n")
								}
							case "EXPUNGE":
								write("* 1 EXPUNGE\r\n" + tag + " OK expunged\r\n")
							case "LOGOUT":
								write("* BYE done\r\n" + tag + " OK logout\r\n")
								return
							default:
								write(tag + " OK done\r\n")
							}
						}
					}()
				}
			}()
			c := New(config.AccountConfig{IMAPHost: "127.0.0.1", IMAPPort: listener.Addr().(*net.TCPAddr).Port, User: "test", Password: "pass"})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := c.Connect(ctx); err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			err = c.MoveMessages(ctx, "INBOX", []uint32{1}, "Archive")
			if copies.Load() != 1 {
				t.Fatalf("COPY repeated: %d", copies.Load())
			}
			if validity == 1 {
				if err != nil || stores.Load() != 1 {
					t.Fatalf("recovery failed: %v stores=%d", err, stores.Load())
				}
			} else {
				var partial *PartialMoveError
				if !errors.As(err, &partial) || !errors.Is(err, ErrUIDValidityChanged) || stores.Load() != 0 {
					t.Fatalf("unsafe recovery: %v stores=%d", err, stores.Load())
				}
			}
		})
	}
}
