package boardclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/asgambat/gospin/internal/sealedcrypto"
)

const (
	// openEndpoint names the route that hands out the envelope of a burn board.
	openEndpoint = "/api/boards/:board/open"

	// sealedModeHeader and sealedBurnHeader ask the plain-text API for a sealed
	// board; the body of that surface is the raw text, so the mode cannot travel
	// inside it.
	sealedModeHeader = "X-Board-Mode"
	sealedBurnHeader = "X-Board-Burn"
)

// SealedKeyError reports an envelope this key cannot open: a wrong key, a
// truncated link or an envelope written for another board.
type SealedKeyError struct {
	Board string
}

// Error explains where the key comes from and what a failure usually means.
func (e *SealedKeyError) Error() string {
	return fmt.Sprintf("board %q: this key does not open the board; check --key, --key-file or GO_SPIN_BOARD_KEY, and that the link is complete to the end of its fragment", e.Board)
}

// Unwrap lets a caller match the crypto failure with errors.Is.
func (e *SealedKeyError) Unwrap() error {
	return sealedcrypto.ErrDecrypt
}

// SealedWrite encrypts text with key and stores the envelope. burn is read only
// when the board is created: the mode and the burn flag are fixed at creation.
func (c *Client) SealedWrite(ctx context.Context, board, text string, key sealedcrypto.Key, burn bool) error {
	envelope, err := sealedcrypto.Seal(key, board, []byte(text))
	if err != nil {
		return err
	}

	headers := make(http.Header, 2)
	headers.Set(sealedModeHeader, sealedModeValue)
	if burn {
		headers.Set(sealedBurnHeader, "true")
	}

	_, err = c.sendWithHeaders(ctx, http.MethodPost, c.boardURL(board), board, boardEndpoint, []byte(envelope), "sealing", headers)
	return err
}

// sealedModeValue is the board mode the API expects on the header. It is the
// string of ghostboard.ModeSealed, repeated here because the client talks HTTP
// and does not import the store.
const sealedModeValue = "sealed"

// SealedRead reads the envelope of a sealed board and decrypts it. A burn board
// answers 409 here: it has to be opened explicitly, with SealedOpen.
func (c *Client) SealedRead(ctx context.Context, board string, key sealedcrypto.Key) (string, error) {
	payload, err := c.fetch(ctx, c.boardURL(board), board, boardEndpoint, "reading")
	if err != nil {
		return "", err
	}
	return decryptBoard(board, string(payload), key)
}

// SealedGetBoard reads the state of a sealed board and decrypts its text, so
// `read --json` still carries the revision and the update time.
func (c *Client) SealedGetBoard(ctx context.Context, board string, key sealedcrypto.Key) (Board, error) {
	state, err := c.GetBoard(ctx, board)
	if err != nil {
		return Board{}, err
	}
	if state.Text == "" {
		return state, nil
	}

	plaintext, err := decryptBoard(board, state.Text, key)
	if err != nil {
		return Board{}, err
	}
	state.Text = plaintext
	return state, nil
}

// SealedOpen opens a burn board once, destroying it, and decrypts the envelope it
// hands out.
func (c *Client) SealedOpen(ctx context.Context, board string, key sealedcrypto.Key) (string, error) {
	payload, err := c.send(ctx, http.MethodPost, c.boardURL(board)+"/open", board, openEndpoint, nil, "opening")
	if err != nil {
		return "", err
	}
	return decryptBoard(board, string(payload), key)
}

// decryptBoard turns an envelope into its text, naming the board when the key
// does not fit.
func decryptBoard(board, envelope string, key sealedcrypto.Key) (string, error) {
	plaintext, err := sealedcrypto.Open(key, board, envelope)
	if err != nil {
		if errors.Is(err, sealedcrypto.ErrDecrypt) {
			return "", &SealedKeyError{Board: board}
		}
		return "", fmt.Errorf("board %q: %w", board, err)
	}
	return string(plaintext), nil
}
