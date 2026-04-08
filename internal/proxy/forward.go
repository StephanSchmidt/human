package proxy

import (
	"context"
	"io"
	"net"

	"github.com/rs/zerolog"
)

// Forward pipes data bidirectionally between client and upstream,
// replaying peeked ClientHello bytes to upstream first.
func Forward(ctx context.Context, client, upstream net.Conn, peeked []byte, logger zerolog.Logger) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 2) //nolint:mnd // two copy directions

	// Replay peeked bytes + client → upstream.
	go func() {
		if len(peeked) > 0 {
			if _, err := upstream.Write(peeked); err != nil {
				errCh <- err
				return
			}
		}
		_, err := io.Copy(upstream, client)
		errCh <- err
	}()

	// upstream → client.
	go func() {
		_, err := io.Copy(client, upstream)
		errCh <- err
	}()

	// Track how many goroutine results we've already seen so we drain
	// exactly what's left. A fixed "drain once" loop silently lost the
	// second direction's error whenever ctx.Done fired before errCh.
	consumed := 0
	select {
	case err := <-errCh:
		consumed++
		if err != nil {
			logger.Debug().Err(err).Msg("forward copy finished with error")
		}
	case <-ctx.Done():
	}

	cancel()
	_ = client.Close()
	_ = upstream.Close()

	for i := consumed; i < 2; i++ {
		if err := <-errCh; err != nil {
			logger.Debug().Err(err).Msg("forward copy (other direction) finished with error")
		}
	}
}
