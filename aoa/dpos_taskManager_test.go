// Copyright 2018 The go-aurora Authors
// This file is part of the go-aurora library.
//
// The go-aurora library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-aurora library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-aurora library. If not, see <http://www.gnu.org/licenses/>.

package aoa

import (
	"fmt"
	"github.com/Aurorachain/go-aoa/core/types"
	"testing"
)

func TestRlpHash(t *testing.T) {
	var list types.ShuffleList
	a1 := types.ShuffleDel{WorkTime: 1, Address: "0x", Vote: 2, Nickname: "node1"}
	list.ShuffleDels = append(list.ShuffleDels, a1)
	// empty hash :0x959684cc863003d5ac5cb31bcf5baf7e1b4fc60963fcc36fbc1bf4394a0e2e3c
	// one ele:0x04e0c0c357b237ae31e30d2b043a0704cc811c9cfa919fb8961db93c99b07cfb
	h := rlpHash(list)
	fmt.Println(h.Hex())
}
