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

// Contains the metrics collected by the fetcher.

package fetcher

import (
	"github.com/Aurorachain/go-aoa/metrics"
)

var (
	propAnnounceInMeter   = metrics.NewMeter("aoa/fetcher/prop/announces/in")
	propAnnounceOutTimer  = metrics.NewTimer("aoa/fetcher/prop/announces/out")
	propAnnounceDropMeter = metrics.NewMeter("aoa/fetcher/prop/announces/drop")
	propAnnounceDOSMeter  = metrics.NewMeter("aoa/fetcher/prop/announces/dos")

	propBroadcastInMeter   = metrics.NewMeter("aoa/fetcher/prop/broadcasts/in")
	propBroadcastOutTimer  = metrics.NewTimer("aoa/fetcher/prop/broadcasts/out")
	propBroadcastDropMeter = metrics.NewMeter("aoa/fetcher/prop/broadcasts/drop")
	propBroadcastDOSMeter  = metrics.NewMeter("aoa/fetcher/prop/broadcasts/dos")

	headerFetchMeter = metrics.NewMeter("aoa/fetcher/fetch/headers")
	bodyFetchMeter   = metrics.NewMeter("aoa/fetcher/fetch/bodies")

	headerFilterInMeter  = metrics.NewMeter("aoa/fetcher/filter/headers/in")
	headerFilterOutMeter = metrics.NewMeter("aoa/fetcher/filter/headers/out")
	bodyFilterInMeter    = metrics.NewMeter("aoa/fetcher/filter/bodies/in")
	bodyFilterOutMeter   = metrics.NewMeter("aoa/fetcher/filter/bodies/out")
)
