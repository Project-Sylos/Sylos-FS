module codeberg.org/Sylos/Sylos-FS

go 1.25.6

require (
	codeberg.org/Sylos/Spectra v0.2.6
	golang.org/x/sys v0.42.0
)

require go.etcd.io/bbolt v1.3.10 // indirect

replace codeberg.org/Sylos/Spectra => ../Spectra
