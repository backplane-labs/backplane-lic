package render

import (
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// Group renders a group exactly as the reference's GroupSerializer
// does.
func Group(group domain.Group) oas.GroupResource {
	base := "/v1/accounts/" + group.AccountID.String()

	many := func(path string) oas.ToManyRelationship {
		return oas.ToManyRelationship{
			Links: oas.RelationshipLinks{Related: oas.NewNilString(base + "/groups/" + group.ID.String() + path)},
		}
	}

	return oas.GroupResource{
		Type: oas.GroupResourceTypeGroups,
		ID:   group.ID.String(),
		Attributes: oas.GroupAttributes{
			Name:        NilString(group.Name),
			MaxUsers:    NilInt(group.MaxUsers),
			MaxLicenses: NilInt(group.MaxLicenses),
			MaxMachines: NilInt(group.MaxMachines),
			Metadata:    Metadata(group.Metadata),
			Created:     Timestamp(group.CreatedAt),
			Updated:     Timestamp(group.UpdatedAt),
		},
		Relationships: oas.GroupRelationships{
			Account: oas.ToOneRelationship{
				Links: oas.RelationshipLinks{Related: oas.NewNilString(base)},
				Data: oas.NilResourceIdentifier{
					Value: oas.ResourceIdentifier{Type: "accounts", ID: group.AccountID.String()},
				},
			},
			Owners:   many("/owners"),
			Users:    many("/users"),
			Licenses: many("/licenses"),
			Machines: many("/machines"),
		},
		Links: oas.ResourceLinks{Self: base + "/groups/" + group.ID.String()},
	}
}

// GroupOwner renders the group-owners join resource.
func GroupOwner(owner domain.GroupOwner) oas.GroupOwnerResource {
	base := "/v1/accounts/" + owner.AccountID.String()

	one := func(kind, id, path string) oas.ToOneRelationship {
		return oas.ToOneRelationship{
			Links: oas.RelationshipLinks{Related: oas.NewNilString(base + path)},
			Data:  oas.NilResourceIdentifier{Value: oas.ResourceIdentifier{Type: kind, ID: id}},
		}
	}

	return oas.GroupOwnerResource{
		Type: oas.GroupOwnerResourceTypeGroupOwners,
		ID:   owner.ID.String(),
		Attributes: oas.GroupOwnerResourceAttributes{
			Created: Timestamp(owner.CreatedAt),
			Updated: Timestamp(owner.UpdatedAt),
		},
		Relationships: oas.GroupOwnerResourceRelationships{
			Account: one("accounts", owner.AccountID.String(), ""),
			Group:   one("groups", owner.GroupID.String(), "/groups/"+owner.GroupID.String()),
			User:    one("users", owner.UserID.String(), "/users/"+owner.UserID.String()),
		},
		Links: oas.ResourceLinks{
			Self: base + "/groups/" + owner.GroupID.String() + "/owners/" + owner.ID.String(),
		},
	}
}

// NilInt renders an optional limit column.
func NilInt(v *int) oas.NilInt {
	if v == nil {
		return oas.NilInt{Null: true}
	}

	return oas.NilInt{Value: *v}
}
