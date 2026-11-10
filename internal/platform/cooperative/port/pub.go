package port

// Published data contracts. No default KN reader is granted in this package.
// PubSchema follows <cluster>_<module>_pub, so modgen resolves ownership without aliases.
// plot_export/v1 (#271): nguồn đọc của migration chép một lần thửa KN sang kho KN (pf 20261110013755).
//
//ag:pubview pf_cooperative_pub.cooperatives v1
//ag:pubview pf_cooperative_pub.plots v1
//ag:pubview pf_cooperative_pub.seasons v1
//ag:pubview pf_cooperative_pub.confirmations v1
//ag:pubview pf_cooperative_pub.plot_export v1
const PubSchema = "pf_cooperative_pub"
