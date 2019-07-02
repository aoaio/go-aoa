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

// Contains the metrics collected by the downloader.

package downloader

import (
	"github.com/Aurorachain/go-aoa/metrics"
)

var (
	headerInMeter      = metrics.NewMeter("aoa/downloader/headers/in")
	headerReqTimer     = metrics.NewTimer("aoa/downloader/headers/req")
	headerDropMeter    = metrics.NewMeter("aoa/downloader/headers/drop")
	headerTimeoutMeter = metrics.NewMeter("aoa/downloader/headers/timeout")

	bodyInMeter      = metrics.NewMeter("aoa/downloader/bodies/in")
	bodyReqTimer     = metrics.NewTimer("aoa/downloader/bodies/req")
	bodyDropMeter    = metrics.NewMeter("aoa/downloader/bodies/drop")
	bodyTimeoutMeter = metrics.NewMeter("aoa/downloader/bodies/timeout")

	receiptInMeter      = metrics.NewMeter("aoa/downloader/receipts/in")
	receiptReqTimer     = metrics.NewTimer("aoa/downloader/receipts/req")
	receiptDropMeter    = metrics.NewMeter("aoa/downloader/receipts/drop")
	receiptTimeoutMeter = metrics.NewMeter("aoa/downloader/receipts/timeout")

	stateInMeter   = metrics.NewMeter("aoa/downloader/states/in")
	stateDropMeter = metrics.NewMeter("aoa/downloader/states/drop")
)
