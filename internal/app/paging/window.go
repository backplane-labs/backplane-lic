package paging

import "github.com/google/uuid"

// Page is the storage-layer list window shared by every repository:
// the cursor (NULL outside keyset mode), the page bounds and the
// direction. Fetch is the row count to read — one more than the page
// in cursor mode, so the row after the page answers whether a next
// page exists.
type Page struct {
	Cursor *uuid.UUID
	Limit  int64
	Offset int64
	Fetch  int64
	Asc    bool
}

// Window projects the parsed params onto the storage window.
func (p Params) Window() Page {
	size := int64(p.SizeOrDefault())

	page := Page{Limit: size, Fetch: size, Asc: p.Order == OrderAsc}

	if p.Mode == ModeOffset {
		page.Offset = int64(p.Number-1) * size
	}

	if p.Mode == ModeCursor {
		page.Fetch = size + 1

		if p.Cursor.Valid {
			id := p.Cursor.UUID
			page.Cursor = &id
		}
	}

	return page
}
