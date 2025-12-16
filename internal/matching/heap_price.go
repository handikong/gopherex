package matching

type minPriceHeap []int64

func (m minPriceHeap) Len() int {
	return len(m)
}

func (m minPriceHeap) Less(i, j int) bool {
	return m[i] < m[j]
}

func (m minPriceHeap) Swap(i, j int) {
	m[i], m[j] = m[j], m[i]
}

func (m *minPriceHeap) Push(x any) {
	*m = append(*m, x.(int64))
}

func (m *minPriceHeap) Pop() any {
	old := *m
	n := len(old)
	x := old[n-1]
	*m = old[:n-1]
	return x
}

type maxPriceHeap []int64

func (m maxPriceHeap) Len() int {
	return len(m)
}

func (m maxPriceHeap) Less(i, j int) bool {
	return m[i] > m[j]
}

func (m maxPriceHeap) Swap(i, j int) {
	m[i], m[j] = m[j], m[i]
}

func (m *maxPriceHeap) Push(x any) {
	*m = append(*m, x.(int64))
}

func (m *maxPriceHeap) Pop() any {
	old := *m
	n := len(old)
	x := old[n-1]
	*m = old[:n-1]
	return x
}
