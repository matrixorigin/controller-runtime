// Copyright 2022 Matrix Origin
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package reconciler

import (
	"fmt"
	"reflect"
	"time"
)

type ReSync struct {
	Message      string
	RequeueAfter time.Duration
}

func (e *ReSync) Error() string {
	return fmt.Sprintf("reconcile error: %s, retry after %s", e.Message, e.RequeueAfter)
}

func ErrReSync(msg string, requeueAfter ...time.Duration) *ReSync {
	e := &ReSync{
		Message: msg,
	}
	if len(requeueAfter) > 0 {
		e.RequeueAfter = requeueAfter[0]
	}
	return e
}

// see: https://go.dev/doc/faq#nil_error
func IsNil(object interface{}) bool {
	if object == nil {
		return true
	}

	value := reflect.ValueOf(object)
	kind := value.Kind()
	if kind >= reflect.Chan && kind <= reflect.Slice && value.IsNil() {
		return true
	}

	return false
}
