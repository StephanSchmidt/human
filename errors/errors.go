package errors

import (
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	goerrors "gitlab.com/tozd/go/errors"
)

func LogError(err error) *zerolog.Event {
	return log.Error().Err(err).Fields(goerrors.AllDetails(err)).Err(err)
}

func WithDetails(message string, details ...interface{}) error {
	args := extractArgs(message, details)
	return goerrors.WithDetails(
		goerrors.Errorf(message, args...),
		details...,
	)
}

func WrapWithDetails(err error, message string, details ...interface{}) error {
	args := extractArgs(message, details)
	return goerrors.WithDetails(
		goerrors.Wrapf(err, message, args...),
		details...,
	)
}

func AllDetails(err error) map[string]interface{} {
	return goerrors.AllDetails(err)
}

func isFormatVerb(c byte) bool {
	switch c {
	case 'b', 'c', 'd', 'e', 'E', 'f', 'F', 'g', 'G', 'o', 'O', 'p', 'q', 's', 't', 'T', 'U', 'v', 'w', 'x', 'X':
		return true
	}
	return false
}

func extractArgs(message string, details []interface{}) []interface{} {
	var args []interface{}
	for i := 1; i < len(details); i += 2 {
		args = append(args, details[i])
	}

	placeholderCount := 0
	for i := 0; i < len(message); i++ {
		if message[i] == '%' && i+1 < len(message) {
			if message[i+1] == '%' {
				i++ // skip escaped %%
			} else if isFormatVerb(message[i+1]) {
				placeholderCount++
			}
		}
	}

	if len(args) > placeholderCount {
		args = args[:placeholderCount]
	}
	return args
}
