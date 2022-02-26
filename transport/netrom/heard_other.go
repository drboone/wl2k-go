// Copyright 2015 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

// +build !libax25

package netrom

import "time"

func Heard(nrPort string) (map[string]time.Time, error) { return nil, ErrNoLibax25 }
