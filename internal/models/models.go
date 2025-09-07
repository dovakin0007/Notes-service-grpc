package models

import "time"

type Actor struct {
	ID          string
	DisplayName *string
	AvatarURL   *string
}

type Attachment struct {
	ID         string
	URL        string
	FileName   string
	FileType   string
	UploadedAt time.Time
	SHA256     *string
	SizeBytes  *int64
	NoteID     string
}

type NoteRevision struct {
	ID       string
	NoteID   string
	Title    string
	Content  string
	EditorID string
	EditedAt time.Time
	Editor   *Actor
}

type Note struct {
	ID          string
	ProjectID   *string
	AuthorID    string
	Title       string
	Content     *string
	IsPinned    bool
	Tags        []string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Author      *Actor
	Revisions   []NoteRevision
	Attachments []Attachment
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
