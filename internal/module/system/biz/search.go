package systembiz

import (
	"context"
	"fmt"
	"time"

	"shopnexus-remastered/internal/client/search"
)

const IntervalSyncSearch = 1 * time.Minute
const BatchSizeSyncSearch = 100

func (b *SystemBiz) SetupSyncSearch() error {
	//ctx := context.Background()
	//go func() {
	//	fmt.Println("🛠️  Starting sync search background worker...")
	//	for {
	//		stales, err := b.storage.ListStaleSyncSearch(ctx, BatchSizeSyncSearch)
	//		if err != nil {
	//			log.Printf("error listing stale sync search: %v", err)
	//			time.Sleep(IntervalSyncSearch)
	//			continue
	//		}
	//		stalesMap := make(map[string][]int64) // map[RefType][]refID
	//		for _, stale := range stales {
	//			stalesMap[stale.RefType] = append(stalesMap[stale.RefType], stale.RefID)
	//		}
	//		for refType, refIDs := range stalesMap {
	//			switch refType {
	//			case "Product":
	//products, err := b.storage.ListProductDetail(ctx, refIDs)
	//				if err != nil {
	//					log.Printf("error listing products for sync search: %v", err)
	//					continue
	//				}
	//				if err := UpdateSearch(ctx, b.search, refIDs, products); err != nil {
	//					log.Printf("error updating search for products: %v", err)
	//					continue
	//				}
	//
	//			default:
	//				log.Printf("unknown ref type for sync search: %s", refType)
	//			}
	//		}
	//
	//		time.Sleep(IntervalSyncSearch)
	//	}
	//}()

	return nil
}

func UpdateSearch[T any](ctx context.Context, search search.Client, ids []int64, docs []T) error {
	if len(ids) != len(docs) {
		return fmt.Errorf("ids and docs length mismatch")
	}
	for i, p := range docs {
		if err := search.UpdateDocument(ctx, "products", fmt.Sprintf("%v", ids[i]), p); err != nil {
			return err
		}
	}
	return nil
}
