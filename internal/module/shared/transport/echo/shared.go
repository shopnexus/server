package sharedecho

import (
	"fmt"
	"github.com/tus/tusd/v2/pkg/filelocker"
	"github.com/tus/tusd/v2/pkg/filestore"
	tusd "github.com/tus/tusd/v2/pkg/handler"
	"log"
	sharedbiz "shopnexus-remastered/internal/module/shared/biz"

	"github.com/labstack/echo/v4"
)

const Endpoint = "/api/v1/shared/upload-file"

type Handler struct {
	biz *sharedbiz.SharedBiz
}

func NewHandler(e *echo.Echo, biz *sharedbiz.SharedBiz) *Handler {
	h := &Handler{biz: biz}
	api := e.Group("/api/v1/shared")
	//

	tusMuxHandler, err := CreateMuxTusHandler()
	if err != nil {
		log.Fatalf("unable to create tus handler: %s", err)
	}
	api.POST("/upload-file", echo.WrapHandler(tusMuxHandler))

	return h
}

func CreateMuxTusHandler() (*tusd.Handler, error) {
	//store := s3store.New(config.GetConfig().S3.Bucket, s3API)
	store := filestore.New("./uploads")

	// A locking mechanism helps preventing data loss or corruption from
	// parallel requests to a upload resource. A good match for the disk-based
	// storage is the filelocker package which uses disk-based file lock for
	// coordinating access.
	// More information is available at https://tus.github.io/tusd/advanced-topics/locks/.
	locker := filelocker.New("./uploads")

	// A storage backend for tusd may consist of multiple different parts which
	// handle upload creation, locking, termination and so on. The composer is a
	// place where all those separated pieces are joined together. In this example
	// we only use the file store but you may plug in multiple.
	composer := tusd.NewStoreComposer()
	store.UseIn(composer)
	locker.UseIn(composer)

	// Create a new HTTP handler for the tusd server by providing a configuration.
	// The StoreComposer property must be set to allow the handler to function.
	//logger.Log.Info(fmt.Sprintf("Tus url: %s", config.GetConfig().App.TusUrl))
	handler, err := tusd.NewHandler(tusd.Config{
		BasePath:                "/",
		StoreComposer:           composer,
		NotifyCompleteUploads:   true,
		NotifyUploadProgress:    true,
		NotifyTerminatedUploads: true,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to create handler: %w", err)
	}

	// Start another goroutine for receiving events from the handler whenever
	// an upload is completed. The event will contains details about the upload
	// itself and the relevant HTTP request.
	go func() {
		for {
			select {
			case event := <-handler.CompleteUploads:
				log.Printf("✅ Upload %s finished\n", event.Upload.ID)
			case progress := <-handler.UploadProgress:
				log.Printf("🔄 Upload %s progress: %d\n", progress.Upload.ID, progress.Upload.Offset)
			case fail := <-handler.TerminatedUploads:
				log.Printf("❌ Upload %s failed: %v\n", fail.Upload.ID, fail.Upload.ID)
			}
		}
	}()

	// Right now, nothing has happened since we need to start the HTTP server on
	// our own. In the end, tusd will start listening on and accept request at
	// http://localhost:8080/files
	// http.Handle("/files/", http.StripPrefix("/files/", handler))
	// http.Handle("/files", http.StripPrefix("/files", handler))
	// err = http.ListenAndServe(":8080", nil)
	// if err != nil {
	// 	log.Fatalf("unable to listen: %s", err)
	// }
	return handler, nil
}
