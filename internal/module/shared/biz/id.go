package biz

func (b *SharedBiz) HashID(id int64) string {
	newId, _ := b.idhash.Encode([]uint64{uint64(id)})
	return newId
}

func (b *SharedBiz) UnhashID(hash string) int64 {
	ids := b.idhash.Decode(hash)
	if len(ids) == 0 {
		return 0
	}
	return int64(ids[0])
}
