// Ca vi phạm: van công bố là một chiều MKG → nền tảng; nền tảng không chú thích //ag:publication.
package port

// Leak dùng hợp đồng van ở vùng nền tảng.
//
//ag:publication depot_site
type Leak struct {
	SiteRef string `json:"site_ref"`
}
