package tests

import (
	"testing"

)

func TestTransaction(t *testing.T) {
	t.Parallel()

	txt := new(testMatcher)

	txt.walk(t, transactionTestDir, func(t *testing.T, name string, test *TransactionTest) {
		cfg := txt.findConfig(name)
		if err := txt.checkFailure(t, name, test.Run(cfg)); err != nil {
			t.Error(err)
		}
	})
}
