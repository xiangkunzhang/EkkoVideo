package hongguo

// v5 分支的表组。
//
// 分组骨架与旧版一致，差异集中在四张表和初始轮常量上：
// 每张表有 4 组，按时间戳低 2 位（word）选取。
var (
	aesV5Cons = [4][4]int{
		{2, 0, 3, 1},
		{1, 3, 2, 0},
		{3, 2, 1, 0},
		{2, 3, 1, 0},
	}
	aesV5Orders = [4][16]int{
		{0, 9, 14, 15, 4, 13, 2, 7, 8, 1, 6, 3, 12, 5, 10, 11},
		{0, 9, 14, 11, 4, 13, 2, 3, 8, 1, 6, 15, 12, 5, 10, 7},
		{0, 9, 14, 3, 4, 13, 2, 15, 8, 1, 6, 11, 12, 5, 10, 7},
		{0, 9, 14, 15, 4, 13, 2, 3, 8, 1, 6, 11, 12, 5, 10, 7},
	}
	aesV5Initials = [4]uint32{0xbbb5de72, 0xd4638d5b, 0x92a5f73b, 0x89f8a1c9}

	// aesV5Wire / aesV5Compact 描述明文字节与密文字节之间的位映射。
	aesV5Wire    = [8]int{6, 5, 3, 4, 2, 7, 1, 0}
	aesV5Compact = [8]int{1, 7, 3, 6, 2, 0, 4, 5}
)

// newAesV3V5 构造 v5 分支的字节变换器。
func newAesV3V5(key []byte, ts uint32) *aesV3 {
	return newAesV3WithTables(key, ts, v5SBox, aesV5Cons, aesV5Orders, aesV5Initials, aesV5Wire, aesV5Compact)
}
