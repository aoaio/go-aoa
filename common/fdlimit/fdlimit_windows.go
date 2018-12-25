package fdlimit

import "errors"

func Raise(max uint64) error {

	if max > 16384 {
		return errors.New("file descriptor limit (16384) reached")
	}
	return nil
}

func Current() (int, error) {

	return 16384, nil
}

func Maximum() (int, error) {
	return Current()
}
