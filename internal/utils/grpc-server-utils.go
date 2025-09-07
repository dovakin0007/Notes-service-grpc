package utils

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"dovakin0007.com/notes-grpc/internal/models"
	pb "dovakin0007.com/notes-grpc/notes"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

var allowedPaths = map[string]struct{}{
	"title":       {},
	"content":     {},
	"tags":        {},
	"is_pinned":   {},
	"attachments": {},
}

var sortWhitelist = map[string]string{
	"created_at": "created_at",
	"updated_at": "updated_at",
	"title":      "title",
	"id":         "id",
}

type NotesPagination struct {
	Key       string `json:"key"`
	KeyType   string `json:"key_type"`
	ID        string `json:"id"`
	SortBy    string `json:"sort_by"`
	Direction string `json:"direction"`
}

type NoteListQuery struct {
	ProjectID *string
	UserID    *string
	Query     *string
	SortBy    string
	SortDesc  bool
	PageSize  int32
	Cursor    *NotesPagination
}

func NormalizeMask(m *fieldmaskpb.FieldMask) error {
	if m == nil || len(m.Paths) == 0 {
		return errors.New("field mask is required")
	}
	for _, p := range m.Paths {
		if _, ok := allowedPaths[p]; !ok {
			return errors.New("invalid field mask path: " + p)
		}
	}

	return nil
}

func UpdatesNotesMask(update_notes *models.UpdateNoteInput, req *pb.UpdateNoteRequest) {
	for _, path := range req.UpdateMask.GetPaths() {
		switch path {
		case "title":
			update_notes.Title = &req.Title
		case "content":
			update_notes.Content = &req.Content
		case "tags":
			update_notes.Tags = &req.Tags
		case "is_pinned":
			update_notes.IsPinned = &req.IsPinned
		case "attachments":
			{
				var attachments []models.Attachment = make([]models.Attachment, 1, 10)
				for _, attachment := range req.Attachments {
					attachments = append(attachments, ProtoToAttachmentModel(attachment))
				}
				update_notes.Attachments = attachments
			}
		case "user":
			{
				actor := ProtoToActorModel(req.User)
				update_notes.Editor = &actor
			}
		}
	}
}

func NormalizeSort(sortBy string) (col string) {
	col = strings.ToLower(sortBy)
	if col == "" {
		col = "updated_at"
	}

	if _, ok := sortWhitelist[col]; !ok {

		col = "updated_at"
	}
	return col
}

func EncodePaginationToken(c NotesPagination) (string, error) {
	b, err := json.Marshal(c)

	if err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(b), nil
}

func DecodePaginationToken(token string) (*NotesPagination, error) {
	var p NotesPagination
	if token == "" {
		return &p, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}

	if p.Key == "" || p.SortBy == "" || p.Direction == "" || p.ID == "" || p.KeyType == "" {
		return &p, errors.New("invalid pagination token")
	}

	return &p, nil

}

func NilIfEmpty(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}
