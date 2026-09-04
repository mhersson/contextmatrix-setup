// Package bootstrap extracts the one-time first-admin link the server logs
// on its first start in auth.mode multi. The server logs a path only; the
// installer composes the URL.
package bootstrap

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"
)

var ErrNoLink = errors.New("no bootstrap link seen")

var linkRe = regexp.MustCompile(`path=(/auth/token/[^\s"]+)`)

func ParseLink(line string) (string, bool) {
	m := linkRe.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}

	return m[1], true
}

func URL(port int, path string) string {
	return fmt.Sprintf("http://localhost:%d%s", port, path)
}

func Wait(ctx context.Context, follow func(ctx context.Context, w io.Writer) error, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	pr, pw := io.Pipe()
	done := make(chan error, 1)

	go func() {
		err := follow(ctx, pw)

		_ = pw.CloseWithError(err)
		done <- err
	}()

	found := make(chan string, 1)

	go func() {
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			if p, ok := ParseLink(sc.Text()); ok {
				found <- p

				return
			}
		}

		close(found)
	}()

	select {
	case p, ok := <-found:
		cancel()

		// A follower blocked in Write does not observe context cancellation;
		// closing the read end unblocks it with io.ErrClosedPipe so it can return.
		_ = pr.Close()

		<-done

		if !ok {
			return "", ErrNoLink
		}

		return p, nil
	case <-ctx.Done():
		_ = pr.Close()

		<-done

		return "", ErrNoLink
	}
}
