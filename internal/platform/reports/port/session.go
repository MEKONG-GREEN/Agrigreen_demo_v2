package port

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/google/uuid"
)

const (
	SessionReadForms     = "forms.read"
	SessionReadDispatch  = "dispatch.read"
	SessionWriteDispatch = "dispatch.write"
)

// SessionActor is an application-owned projection. Actor and tenant identity
// always come from Principal. A handled projection must explicitly grant the
// operation and provide a department filter; zero values deny access.
type SessionActor struct {
	Allowed          bool
	Department       string
	AllDepartments   bool
	AllProvinces     bool
	Province         string
	AllowsDepartment func(string) bool
	UnmaskRecipients bool
	OnBehalfOf       *uuid.UUID
}

// SessionResolver is registered at assembly. Error or handled denial is final;
// handled=false allows legacy callers during the registration transition.
type SessionResolver func(context.Context, authz.Principal, string) (SessionActor, bool, error)
