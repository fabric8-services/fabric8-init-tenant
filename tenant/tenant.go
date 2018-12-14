package tenant

import (
	"strings"
	"time"

	"database/sql/driver"
	"github.com/fabric8-services/fabric8-tenant/environment"
	"github.com/satori/go.uuid"
)

// Tenant is the owning OpenShift account
type Tenant struct {
	ID         uuid.UUID `sql:"type:uuid" gorm:"primary_key"` // This is the ID PK field
	CreatedAt  time.Time
	UpdatedAt  time.Time
	DeletedAt  *time.Time
	Email      string
	Profile    string
	OSUsername string
	NsBaseName string
}

// TableName overrides the table name settings in Gorm to force a specific table name
// in the database.
func (m Tenant) TableName() string {
	return "tenants"
}

// Namespace represent a single namespace owned by an Tenant
type Namespace struct {
	ID        uuid.UUID `sql:"type:uuid default uuid_generate_v4()" gorm:"primary_key"`
	TenantID  uuid.UUID `sql:"type:uuid"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
	Name      string
	MasterURL string
	Type      environment.Type
	Version   string
	State     NamespaceState
	UpdatedBy string
}

// TableName overrides the table name settings in Gorm to force a specific table name
// in the database.
func (m Namespace) TableName() string {
	return "namespaces"
}

// GetNamespaceType attempts to extract the namespace type based on namespace name
func GetNamespaceType(name, nsBaseName string) environment.Type {
	if name == nsBaseName {
		return environment.TypeUser
	}
	if strings.HasSuffix(name, "-jenkins") {
		return environment.TypeJenkins
	}
	if strings.HasSuffix(name, "-che") {
		return environment.TypeChe
	}
	if strings.HasSuffix(name, "-test") {
		return environment.TypeTest
	}
	if strings.HasSuffix(name, "-stage") {
		return environment.TypeStage
	}
	if strings.HasSuffix(name, "-run") {
		return environment.TypeRun
	}
	return environment.TypeCustom
}

type NamespaceState string

const (
	Provisioning NamespaceState = "provisioning"
	Updating     NamespaceState = "updating"
	Ready        NamespaceState = "ready"
	Failed       NamespaceState = "failed"
)

func (s NamespaceState) String() string {
	return string(s)
}

// Value - Implementation of valuer for database/sql
func (ns *NamespaceState) Value() (driver.Value, error) {
	return string(*ns), nil
}

// Scan - Implement the database/sql scanner interface
func (ns *NamespaceState) Scan(value interface{}) error {
	if value == nil {
		*ns = NamespaceState(Ready)
		return nil
	}
	if bv, err := driver.String.ConvertValue(value); err == nil {
		// if this is a bool type
		if v, ok := bv.(string); ok {
			// set the value of the pointer yne to YesNoEnum(v)
			*ns = NamespaceState(v)
			return nil
		}
	}
	// otherwise, set ready state
	*ns = Ready
	return nil
}
