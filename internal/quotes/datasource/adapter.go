package datasource

// 定义适配器

type Adapter interface {
	Register()
	Unregister()
}
