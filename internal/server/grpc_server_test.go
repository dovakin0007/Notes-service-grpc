package server_test

import (
	"context"
	"net"
	"testing"
	"time"

	"dovakin0007.com/notes-grpc/internal/models"
	"dovakin0007.com/notes-grpc/internal/server"
	pb "dovakin0007.com/notes-grpc/notes"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const bufSize = 1024 * 1024

// a simple mock store implementing Store
type mockStore struct {
	// you can add fields to control behavior per-test
	createdNote *models.Note
	// allow simulating errors
	createErr error
	viewErr   error
}

func (m *mockStore) CreateNote(ctx context.Context, in models.CreateNoteInput) (*models.Note, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	// return a sample note that would be stored
	now := time.Now()
	n := &models.Note{
		ID:        uuid.NewString(),
		ProjectID: in.ProjectID,
		AuthorID:  in.Author.ID, // adapt depending on your model types
		Title:     in.Title,
		Content:   in.Content,
		IsPinned:  false,
		Tags:      in.Tags,
		CreatedAt: now,
		UpdatedAt: now,
	}
	m.createdNote = n
	return n, nil
}

func (m *mockStore) UpdateNote(ctx context.Context, in models.UpdateNoteInput) (*models.Note, error) {
	// not needed for this test; add similar behavior when testing UpdateNote
	return nil, nil
}

func (m *mockStore) ListNotes(ctx context.Context, in models.ListNotesFilter) ([]models.Note, string, error) {
	return nil, "", nil
}

func (m *mockStore) ViewNote(ctx context.Context, id string, opts models.GetNoteOptions) (*models.Note, error) {
	if m.viewErr != nil {
		return nil, m.viewErr
	}
	// if we have a createdNote, return it; otherwise fake a note
	if m.createdNote != nil && m.createdNote.ID == id {
		return m.createdNote, nil
	}
	// return a sample note
	now := time.Now()
	return &models.Note{
		ID:        id,
		ProjectID: nil,
		AuthorID:  "user-1",
		Title:     "Existing note",
		Content:   nil,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (m *mockStore) DeleteNote(ctx context.Context, id string, hard bool) (bool, error) {
	return true, nil
}

func dialerWithServer(t *testing.T, s *grpc.Server) func(context.Context, string) (net.Conn, error) {
	l := bufconn.Listen(bufSize)
	go func() {
		if err := s.Serve(l); err != nil {
			// Report error to test
			t.Logf("server.Serve: %v", err)
		}
	}()
	return func(ctx context.Context, _ string) (net.Conn, error) {
		return l.Dial()
	}
}
func buildCreateNoteRequest() *pb.CreateNoteRequest {
	return &pb.CreateNoteRequest{
		ProjectId: ptrString("proj-123"),
		Title:     "My first note",          // plain string (required)
		Content:   ptrString("hello world"), // optional string
		IsPinned:  true,
		Tags:      []string{"go", "grpc", "test"},
		Attachments: []*pb.Attachment{
			{
				Id:         "att-1",
				Url:        "https://example.com/file1",
				FileName:   "file1.txt",
				FileType:   "text/plain",
				UploadedAt: timestamppb.New(time.Now()),
				Sha256:     ptrString("abc123"),
				SizeBytes:  proto.Int64(1024),
			},
		},
		Author: &pb.ActorRef{
			Id:          "user-1", // required
			DisplayName: ptrString("Alice"),
			AvatarUrl:   ptrString("https://example.com/avatar.png"),
		},
		IdempotencyKey: ptrString("idem-123"),
	}
}

func TestCreateAndGetNote(t *testing.T) {
	mock := &mockStore{}

	srv := grpc.NewServer()
	svc := server.NewNoteServiceServerWithStore(mock)
	pb.RegisterNoteServiceServer(srv, svc)

	ctx := context.Background()
	conn, err := grpc.DialContext(
		ctx,
		"bufnet",
		grpc.WithContextDialer(dialerWithServer(t, srv)),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	assert.NoError(t, err)
	defer conn.Close()

	client := pb.NewNoteServiceClient(conn)

	createReq := buildCreateNoteRequest()
	createResp, err := client.CreateNote(ctx, createReq)
	assert.NoError(t, err)
	assert.NotNil(t, createResp.GetNote())
	assert.NotEmpty(t, createResp.GetNote().GetId())

	noteID := createResp.GetNote().GetId()

	getReq := &pb.GetNoteRequest{Id: noteID}
	getResp, err := client.GetNote(ctx, getReq)
	assert.NoError(t, err)
	assert.Equal(t, noteID, getResp.GetNote().GetId())
}

func ptrString(s string) *string { return &s }

func ptrBool(b bool) *bool { return &b }

func ptrInt64(i int64) *int64 {
	return &i
}
