// Copyright (c) 2013-2014 The btcsuite developers
// Copyright (c) 2015 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package legacyrpc

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestThrottle(t *testing.T) {
	const threshold = 1
	busy := make(chan struct{})

	srv := httptest.NewServer(throttledFn(threshold,
		func(w http.ResponseWriter, r *http.Request) {
			<-busy
		}),
	)

	type resp struct {
		resp *http.Response
		err  error
	}
	responses := make(chan resp, 2)
	for i := 0; i < cap(responses); i++ {
		go func() {
			r, err := http.Get(srv.URL)
			responses <- resp{r, err}
		}()
	}

	got := make(map[int]int, cap(responses))
	for i := 0; i < cap(responses); i++ {
		r := <-responses
		if r.err != nil {
			t.Fatal(r.err)
		}
		got[r.resp.StatusCode]++

		if i == 0 {
			close(busy)
		}
	}

	want := map[int]int{200: 1, 429: 1}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("status codes: want: %v, got: %v", want, got)
	}
}
