// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package agentcore

import (
	"context"
	"errors"
)

func ctxCanceled(err error) bool { return errors.Is(err, context.Canceled) }
func ctxDeadline(err error) bool { return errors.Is(err, context.DeadlineExceeded) }
