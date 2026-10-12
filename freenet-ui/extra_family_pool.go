package main

import "sort"

// indexedExtraPoolItem retains subscription order without keeping unbounded
// numbers of profiles on small Entware routers.
type indexedExtraPoolItem[T any] struct {
	Value T
	Index int
}

// cappedExtraFamilyPool keeps both allowed Extra families discoverable even
// when a subscription lists every ordinary Extra before every Whitelist2.
// The cap is unchanged. Unused family quota returns to the other family.
func cappedExtraFamilyPool[T any](cap int, ordinary, whitelist2 []indexedExtraPoolItem[T]) []T {
	if cap <= 0 {
		return nil
	}
	if len(ordinary) > cap {
		ordinary = ordinary[:cap]
	}
	if len(whitelist2) > cap {
		whitelist2 = whitelist2[:cap]
	}
	combined := append(append(make([]indexedExtraPoolItem[T], 0, len(ordinary)+len(whitelist2)), ordinary...), whitelist2...)
	sort.SliceStable(combined, func(i, j int) bool { return combined[i].Index < combined[j].Index })
	keep := make(map[int]bool, cap)
	if len(ordinary) > 0 && len(whitelist2) > 0 {
		quota := cap / 2
		if quota < 1 {
			quota = 1
		}
		for _, family := range [][]indexedExtraPoolItem[T]{ordinary, whitelist2} {
			for i := 0; i < len(family) && i < quota && len(keep) < cap; i++ {
				keep[family[i].Index] = true
			}
		}
	}
	for _, item := range combined {
		if len(keep) >= cap {
			break
		}
		keep[item.Index] = true
	}
	out := make([]T, 0, len(keep))
	for _, item := range combined {
		if keep[item.Index] {
			out = append(out, item.Value)
		}
	}
	return out
}
