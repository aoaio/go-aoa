package jsre

import (
	"os"
	"reflect"
	"testing"
	"regexp"
	"fmt"
	"github.com/Aurorachain/go-Aurora/common"
	"strings"
)

func TestCompleteKeywords(t *testing.T) {
	re := New("", os.Stdout)
	re.Run(`
		function theClass() {
			this.foo = 3;
			this.gazonk = {xyz: 4};
		}
		theClass.prototype.someMethod = function () {};
  		var x = new theClass();
  		var y = new theClass();
		y.someMethod = function override() {};
	`)

	var tests = []struct {
		input string
		want  []string
	}{
		{
			input: "x",
			want:  []string{"x."},
		},
		{
			input: "x.someMethod",
			want:  []string{"x.someMethod("},
		},
		{
			input: "x.",
			want: []string{
				"x.constructor",
				"x.foo",
				"x.gazonk",
				"x.someMethod",
			},
		},
		{
			input: "y.",
			want: []string{
				"y.constructor",
				"y.foo",
				"y.gazonk",
				"y.someMethod",
			},
		},
		{
			input: "x.gazonk.",
			want: []string{
				"x.gazonk.constructor",
				"x.gazonk.hasOwnProperty",
				"x.gazonk.isPrototypeOf",
				"x.gazonk.propertyIsEnumerable",
				"x.gazonk.toLocaleString",
				"x.gazonk.toString",
				"x.gazonk.valueOf",
				"x.gazonk.xyz",
			},
		},
	}
	for _, test := range tests {
		cs := re.CompleteKeywords(test.input)
		if !reflect.DeepEqual(cs, test.want) {
			t.Errorf("wrong completions for %q\ngot  %v\nwant %v", test.input, cs, test.want)
		}
	}
}

func TestJSRE_CompleteKeywords(t *testing.T) {
	match, _ := regexp.MatchString("[^0-9A-Za-z]", "AOA99d3f195c67cc30c6a20ad760d1ce0d8c295d635")
	fmt.Println(match)
}

func TestJSRE_Bind(t *testing.T) {
	a := common.HexToAddress("5ccb115ac633e2ccfebe65fc4e18e75ce78642b4")
	b := common.HexToAddress(strings.ToTitle("5ccb115ac633e2ccfebe65fc4e18e75ce78642b4"))
	c := common.HexToAddress("5CCB115ac633e2ccfebe65fc4e18e75ce78642b4")
	fmt.Println(a == b)
	fmt.Println(a == c)
}
