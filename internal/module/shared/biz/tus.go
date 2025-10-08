package sharedbiz

import (
	"context"
	"fmt"
	"log"
	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/db"
	authclaims "shopnexus-remastered/internal/module/auth/biz/claims"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	"shopnexus-remastered/internal/utils/pgutil"

	"github.com/tus/tusd/v2/pkg/filelocker"
	"github.com/tus/tusd/v2/pkg/filestore"
	tusd "github.com/tus/tusd/v2/pkg/handler"
)

type TusEventContext struct {
	context.Context
	Account authmodel.AuthenticatedAccount
}

func (b *SharedBiz) NewTusHandler() (*tusd.Handler, error) {
	// store := s3store.New(config.GetConfig().S3.Bucket, s3API)
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
		NotifyCreatedUploads:    true,
		NotifyCompleteUploads:   true,
		NotifyTerminatedUploads: true,
		DisableDownload:         true,
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
			case created := <-handler.CreatedUploads:
				b.OnCompleteUpload(created)
			case event := <-handler.CompleteUploads:
				b.OnCompleteUpload(event)
			case terminated := <-handler.TerminatedUploads:
				b.OnTerminateUpload(terminated)
			}
		}
	}()

	return handler, nil
}

func (b *SharedBiz) NewTusEventContext(event tusd.HookEvent) TusEventContext {
	claims, _ := authclaims.GetClaimsByHeader(event.HTTPRequest.Header)

	return TusEventContext{
		Context: event.Context,
		Account: claims.Account,
	}
}

func (b *SharedBiz) OnCreatedUpload(event tusd.HookEvent) {
	log.Printf("➕ Upload %s created\n", event.Upload.ID)

	ctx := b.NewTusEventContext(event)

	fmt.Println("metadata", event.Upload.MetaData)

	_, err := b.storage.CreateDefaultSharedResource(event.Context, db.CreateDefaultSharedResourceParams{
		UploadedBy: pgutil.Int64ToPgInt8(ctx.Account.ID),
		Provider:   db.SharedResourceProviderLocal,
		Mime:       event.Upload.MetaData["filetype"],
		FileSize:   pgutil.Int64ToPgInt8(event.Upload.Size),
	})
	if err != nil {
		log.Fatalf("error while inserting resource to db %w", err)
	}

	log.Printf("🆔 Resource %s is created\n", event.Upload.ID)
}

func (b *SharedBiz) OnCompleteUpload(event tusd.HookEvent) {
	log.Printf("✅ Upload %s finished\n", event.Upload.ID)

	ctx := b.NewTusEventContext(event)

	rs, err := b.storage.GetSharedResource(ctx, db.GetSharedResourceParams{
		Code: pgutil.StringToPgText(event.Upload.ID),
	})
	if err != nil {
		log.Fatalf("error while getting resource to db %w", err)
	}

	_, err = b.storage.UpdateSharedResource(ctx, db.UpdateSharedResourceParams{
		ID:     rs.ID,
		Status: db.NullSharedStatus{SharedStatus: db.SharedStatusSuccess, Valid: true},
	})
	if err != nil {
		log.Fatalf("error while updating resource to db %w", err)
	}

	log.Printf("🎉 Resource %s is ready\n", event.Upload.ID)
}

func (b *SharedBiz) OnTerminateUpload(event tusd.HookEvent) {
	log.Printf("❌ Upload %s terminated\n", event.Upload.ID)

	ctx := b.NewTusEventContext(event)

	if err := b.storage.DeleteSharedResource(ctx, db.DeleteSharedResourceParams{
		Code: []string{event.Upload.ID},
	}); err != nil {
		log.Fatalf("error while deleting resource to db %w", err)
	}

	log.Printf("🗑️  Resource %s deleted\n", event.Upload.ID)
}

func GetResourceURL(resourceCode string) string {
	switch config.GetConfig().Filestore.Type {
	case "local":
		return fmt.Sprintf("%s/api/v1/shared/files/%s", config.GetConfig().App.PublicURL, resourceCode)
	case "s3":
		return fmt.Sprintf("https://%s/%s", config.GetConfig().Filestore.S3.CloudfrontURL, resourceCode)
	default:
		return "" // TODO: add 404 link
	}
}
