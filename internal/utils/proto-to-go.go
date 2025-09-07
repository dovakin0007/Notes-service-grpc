package utils

import (
	"time"

	"dovakin0007.com/notes-grpc/internal/models"
	pb "dovakin0007.com/notes-grpc/notes"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// NOTE: This whole code is just to convert types
func ToCreateNoteInput(req *pb.CreateNoteRequest) models.CreateNoteInput {

	id := uuid.NewString()

	if req == nil {
		return models.CreateNoteInput{ID: id}
	}

	var projectID *string
	if p := req.GetProjectId(); p != "" {
		projectID = &p
	}

	var content *string
	if c := req.GetContent(); c != "" {
		content = &c
	}

	var idem *string
	if k := req.GetIdempotencyKey(); k != "" {
		idem = &k
	}

	attachments := attachmentsProtoToModel(req.GetAttachments(), id)

	// author -> models.Actor (DisplayName & AvatarURL are *string)
	var author models.Actor
	if req.Author != nil {
		author = models.Actor{
			ID:          req.Author.GetId(),
			DisplayName: strPtrOrNil(req.Author.GetDisplayName()),
			AvatarURL:   strPtrOrNil(req.Author.GetAvatarUrl()),
		}
	}

	return models.CreateNoteInput{
		ID:             id,
		ProjectID:      projectID,
		Title:          req.GetTitle(),
		Content:        content,
		Tags:           req.GetTags(),
		Author:         author,
		Attachment:     attachments,
		IdempotencyKey: idem,
	}
}

func attachmentsProtoToModel(in []*pb.Attachment, noteID string) []models.Attachment {
	if len(in) == 0 {
		return nil
	}
	out := make([]models.Attachment, 0, len(in))
	for _, a := range in {
		if a == nil {
			continue
		}
		var uploaded time.Time
		if a.GetUploadedAt() != nil {
			uploaded = a.GetUploadedAt().AsTime()
		}

		var shaPtr *string
		if s := a.GetSha256(); s != "" {
			shaPtr = &s
		}

		var sizePtr *int64
		if sz := a.GetSizeBytes(); sz != 0 {
			sizePtr = &sz
		}

		out = append(out, models.Attachment{
			ID:         a.GetId(),
			URL:        a.GetUrl(),
			FileName:   a.GetFileName(),
			FileType:   a.GetFileType(),
			UploadedAt: uploaded,
			SHA256:     shaPtr,
			SizeBytes:  sizePtr,
			NoteID:     noteID,
		})
	}
	return out
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func NoteToProto(n models.Note) *pb.Note {
	var projectID *string
	if n.ProjectID != nil && *n.ProjectID != "" {
		projectID = n.ProjectID
	}
	var content *string
	if n.Content != nil && *n.Content != "" {
		content = n.Content
	}

	var authorProto *pb.ActorRef
	if n.Author != nil {
		authorProto = ActorModelToProto(*n.Author)
	}

	var pbAtts []*pb.Attachment
	if len(n.Attachments) > 0 {
		pbAtts = make([]*pb.Attachment, 0, len(n.Attachments))
		for _, a := range n.Attachments {
			pbAtts = append(pbAtts, AttachmentModelToProto(a))
		}
	}

	var pbRevs []*pb.NoteRevision
	if len(n.Revisions) > 0 {
		pbRevs = make([]*pb.NoteRevision, 0, len(n.Revisions))
		for _, r := range n.Revisions {
			pbRevs = append(pbRevs, revisionModelToProto(r))
		}
	}

	var createdAt *timestamppb.Timestamp
	if !n.CreatedAt.IsZero() {
		createdAt = timestamppb.New(n.CreatedAt)
	}
	var updatedAt *timestamppb.Timestamp
	if !n.UpdatedAt.IsZero() {
		updatedAt = timestamppb.New(n.UpdatedAt)
	}

	return &pb.Note{
		Id:          n.ID,
		ProjectId:   projectID,
		Author:      authorProto,
		Title:       n.Title,
		Content:     content,
		IsPinned:    n.IsPinned,
		Tags:        n.Tags,
		Revisions:   pbRevs,
		Attachments: pbAtts,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}
}

// model -> proto Actor
func ActorModelToProto(a models.Actor) *pb.ActorRef {
	if a.ID == "" && (a.DisplayName == nil || *a.DisplayName == "") && (a.AvatarURL == nil || *a.AvatarURL == "") {
		return nil
	}

	return &pb.ActorRef{
		Id:          a.ID,
		DisplayName: strPtrOrNil(*a.DisplayName),
		AvatarUrl:   strPtrOrNil(*a.AvatarURL),
	}
}

// model -> proto Attachment
func AttachmentModelToProto(a models.Attachment) *pb.Attachment {
	var uploaded *timestamppb.Timestamp
	if !a.UploadedAt.IsZero() {
		uploaded = timestamppb.New(a.UploadedAt)
	}
	return &pb.Attachment{
		Id:         a.ID,
		Url:        a.URL,
		FileName:   a.FileName,
		FileType:   a.FileType,
		UploadedAt: uploaded,
		Sha256:     strPtrOrNil(*a.SHA256),
		SizeBytes:  a.SizeBytes,
	}
}

// proto -> model: Actor
func ProtoToActorModel(a *pb.ActorRef) models.Actor {
	if a == nil {
		return models.Actor{}
	}

	var displayName *string
	if a.DisplayName != nil && *a.DisplayName != "" {
		displayName = a.DisplayName
	}

	var avatarURL *string
	if a.AvatarUrl != nil && *a.AvatarUrl != "" {
		avatarURL = a.AvatarUrl
	}

	return models.Actor{
		ID:          a.Id,
		DisplayName: displayName,
		AvatarURL:   avatarURL,
	}
}

// proto -> model: Attachment
func ProtoToAttachmentModel(a *pb.Attachment) models.Attachment {
	if a == nil {
		return models.Attachment{}
	}

	var uploadedAt time.Time
	if a.UploadedAt != nil {
		uploadedAt = a.UploadedAt.AsTime()
	}

	// a.Sha256 and a.SizeBytes are pointer types in your proto -> directly map them
	return models.Attachment{
		ID:         a.Id,
		URL:        a.Url,
		FileName:   a.FileName,
		FileType:   a.FileType,
		UploadedAt: uploadedAt,
		SHA256:     a.Sha256,
		SizeBytes:  a.SizeBytes,
	}
}

func revisionModelToProto(r models.NoteRevision) *pb.NoteRevision {
	var editedAt *timestamppb.Timestamp
	if !r.EditedAt.IsZero() {
		editedAt = timestamppb.New(r.EditedAt)
	}
	var editorProto *pb.ActorRef
	if r.Editor != nil {
		editorProto = ActorModelToProto(*r.Editor)
	}
	return &pb.NoteRevision{
		Id:       r.ID,
		Title:    r.Title,
		Content:  r.Content,
		Editor:   editorProto,
		EditedAt: editedAt,
	}
}

func ProtoToListNotesFilter(req *pb.ListNotesRequest) models.ListNotesFilter {
	filter := models.ListNotesFilter{
		ProjectID: req.ProjectId,
		UserID:    req.UserId,
		Query:     req.Query,
		PageSize:  int(req.PageSize),
		PageToken: req.PageToken,
	}

	if req.SortBy != nil {
		filter.SortBy = *req.SortBy
	} else {
		filter.SortBy = "updated_at"
	}

	if req.SortDesc != nil {
		filter.SortDesc = *req.SortDesc
	}

	if filter.PageSize < 10 {
		filter.PageSize = 10
	} else if filter.PageSize > 100 {
		filter.PageSize = 100
	}

	return filter
}
