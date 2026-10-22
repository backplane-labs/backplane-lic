package domain

// Role of a bearer. The zero value is the plain user role.
type Role string

// The CE roles. Stored underscored like Keygen's internal names; the
// wire format renders them dasherized (see Wire).
const (
	RoleUser        Role = "user"
	RoleAdmin       Role = "admin"
	RoleDeveloper   Role = "developer"
	RoleReadOnly    Role = "read_only"
	RoleSalesAgent  Role = "sales_agent"
	RoleSupportRole Role = "support_agent"
	RoleProduct     Role = "product"
	RoleLicense     Role = "license"
)

// Wire renders the role as the API does: dasherized ("read-only",
// "sales-agent").
func (r Role) Wire() string {
	switch r {
	case RoleReadOnly:
		return "read-only"
	case RoleSalesAgent:
		return "sales-agent"
	case RoleSupportRole:
		return "support-agent"
	default:
		return string(r)
	}
}

// ParseRole parses a wire (dasherized) or stored (underscored) role name.
func ParseRole(s string) (Role, bool) {
	switch s {
	case "user":
		return RoleUser, true
	case "admin":
		return RoleAdmin, true
	case "developer":
		return RoleDeveloper, true
	case "read_only", "read-only":
		return RoleReadOnly, true
	case "sales_agent", "sales-agent":
		return RoleSalesAgent, true
	case "support_agent", "support-agent":
		return RoleSupportRole, true
	case "product":
		return RoleProduct, true
	case "license":
		return RoleLicense, true
	default:
		return "", false
	}
}
