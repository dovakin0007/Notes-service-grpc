package server

type Server interface {
	Run(in <-chan bool)
	End(out chan<- bool)
}

type server[T Server] struct {
	RegisterServer T
	GrpcServer     T
}

func CreateAndStartServer() {
	service := newRegisterServer("notes-grpc", "notes-grpc-service", "localhost", uint(9096))
	// TODO: channels are needed to make it non blocking we can remove it when there is no need
	ch := make(chan bool, 2)
	go service.Run(ch)

	server := NewGrpcServer(9096)
	go server.Run(ch)

	<-ch
	<-ch

}
