package models

import "time"

type Note struct {
	ID          string         `db:"id"`
	ProjectID   *string        `db:"project_id"`
	AuthorID    string         `db:"author_id"`
	Title       string         `db:"title"`
	Content     *string        `db:"content"`
	IsPinned    bool           `db:"is_pinned"`
	Tags        []string       `db:"-"`
	CreatedAt   time.Time      `db:"created_at"`
	UpdatedAt   time.Time      `db:"updated_at"`
	Author      *Actor         `db:"-"`
	Revisions   []NoteRevision `db:"-"`
	Attachments []Attachment   `db:"-"`
}

type Actor struct {
	ID          string  `db:"id"`
	DisplayName *string `db:"display_name"`
	AvatarURL   *string `db:"avatar_url"`
}

type Attachment struct {
	ID         string    `db:"id"`
	URL        string    `db:"url"`
	FileName   string    `db:"file_name"`
	FileType   string    `db:"file_type"`
	UploadedAt time.Time `db:"uploaded_at"`
	SHA256     *string   `db:"sha256"`
	SizeBytes  *int64    `db:"size_bytes"`
	NoteID     string    `db:"note_id"`
}

type NoteRevision struct {
	ID       string    `db:"id"`
	NoteID   string    `db:"note_id"`
	Title    string    `db:"title"`
	Content  string    `db:"content"`
	EditorID string    `db:"editor_id"`
	EditedAt time.Time `db:"edited_at"`
	Editor   *Actor    `db:"-"` // nested struct, filled separately from join
}

type CreateNoteInput struct {
	ID             string
	ProjectID      *string
	Title          string
	Content        *string
	Tags           []string
	Author         Actor
	Attachment     []Attachment
	IdempotencyKey *string
}

type UpdateNoteInput struct {
	NoteID           string
	Title            *string
	Content          *string
	Tags             *[]string
	IsPinned         *bool
	IfMatchUpdatedAt *time.Time
	Editor           *Actor
	CreateRevision   bool
	Attachments      []Attachment
}

type GetNoteOptions struct {
	IncludeRevisions   bool
	IncludeAttachments bool
}

type ListNotesFilter struct {
	ProjectID *string
	UserID    *string // author_id
	Query     *string // full-text search across title+content
	SortBy    string  // "updated_at", "created_at", "title", "is_pinned"
	SortDesc  bool
	PageSize  int
	PageToken string
}
