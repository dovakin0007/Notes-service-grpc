package server

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"

	"dovakin0007.com/notes-grpc/internal/database"
	"dovakin0007.com/notes-grpc/internal/models"
	"dovakin0007.com/notes-grpc/internal/utils"
	pb "dovakin0007.com/notes-grpc/notes"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

type GrpcServer struct {
	Addr         string
	grpcServer   *grpc.Server
	healthServer *health.Server
}

type noteServiceServer struct {
	pb.UnimplementedNoteServiceServer

	db *database.Database
}

func newNoteServiceServer() *noteServiceServer {
	return &noteServiceServer{
		db: database.GetDb(),
	}
}

func (s *noteServiceServer) CreateNote(ctx context.Context, req *pb.CreateNoteRequest) (*pb.NoteResponse, error) {
	if req == nil || req.Title == "" || req.Author == nil {
		return nil, status.Error(codes.InvalidArgument, "project_id and title are required")
	}

	input := utils.ToCreateNoteInput(req)
	note, err := s.db.CreateNote(ctx, input)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, status.Error(codes.NotFound, "failed to create note")
		}
		return nil, status.Errorf(codes.Internal, "unable to insert into db: %v", err)
	}

	if note == nil {
		return nil, status.Error(codes.Internal, "note creation failed")
	}

	return &pb.NoteResponse{
		Note: utils.NoteToProto(*note),
	}, nil
}

func (s *noteServiceServer) UpdateNote(ctx context.Context, req *pb.UpdateNoteRequest) (*pb.NoteResponse, error) {

	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "the update request is empty")
	}
	var noteUpdate models.UpdateNoteInput
	utils.UpdatesNotesMask(&noteUpdate, req)
	note, err := s.db.UpdateNote(ctx, noteUpdate)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, status.Error(codes.NotFound, "note not found")
		}
		return nil, status.Errorf(codes.Internal, "failed to update note: %v", err)
	}
	if note == nil {
		return nil, status.Error(codes.NotFound, "note not found")
	}
	return &pb.NoteResponse{
		Note: utils.NoteToProto(*note),
	}, nil

}

func (s *noteServiceServer) ListNotes(c context.Context, req *pb.ListNotesRequest) (*pb.ListNotesResponse, error) {

	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "the input request was empty")
	}
	listNotesReq := utils.ProtoToListNotesFilter(req)
	notes, token, err := s.db.ListNotes(c, listNotesReq)
	if err != nil {
		return nil, err
	}

	var protoNotes []*pb.Note = make([]*pb.Note, 0)
	for _, note := range notes {
		protoNotes = append(protoNotes, utils.NoteToProto(note))
	}
	return &pb.ListNotesResponse{
		Notes:         protoNotes,
		NextPageToken: token,
	}, nil

}

func (s *noteServiceServer) GetNote(c context.Context, noteRequest *pb.GetNoteRequest) (*pb.NoteResponse, error) {
	id := noteRequest.GetId()

	opts := models.GetNoteOptions{
		IncludeRevisions:   noteRequest.GetIncludeRevisions(),
		IncludeAttachments: noteRequest.GetIncludeAttachments(),
	}
	note, err := s.db.ViewNote(c, id, opts)

	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unable to fetch from db: %s", err.Error())
	}

	return &pb.NoteResponse{
		Note: utils.NoteToProto(*note),
	}, nil
}

func (s *noteServiceServer) DeleteNote(c context.Context, noteRequest *pb.DeleteNoteRequest) (*pb.DeleteNoteResponse, error) {
	id := noteRequest.GetNoteId()
	val, err := s.db.DeleteNote(c, id, true)
	if err != nil {
		return nil, err
	}

	return &pb.DeleteNoteResponse{
		Success: val,
	}, nil
}

func NewGrpcServer(addr int) *GrpcServer {

	newAddr := flag.Int("port", addr, "The server port")
	return &GrpcServer{
		Addr:         fmt.Sprintf(":%d", *newAddr),
		grpcServer:   grpc.NewServer(),
		healthServer: health.NewServer(),
	}
}

func (g *GrpcServer) Run(in chan<- bool) {

	lis, err := net.Listen("tcp", g.Addr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	pb.RegisterNoteServiceServer(g.grpcServer, newNoteServiceServer())

	grpc_health_v1.RegisterHealthServer(g.grpcServer, g.healthServer)
	g.healthServer.SetServingStatus("notes-grpc-service", grpc_health_v1.HealthCheckResponse_SERVING)

	log.Printf("gRPC server running on %s", g.Addr)
	if err := g.grpcServer.Serve(lis); err != nil {
		g.healthServer.SetServingStatus("notes-grpc-service", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
		log.Fatalf("failed to serve: %v", err)
	}
	in <- true
}

func (g *GrpcServer) End(out <-chan bool) {
	log.Println("🛑 Stopping gRPC server")
	g.grpcServer.GracefulStop()
	<-out
}
