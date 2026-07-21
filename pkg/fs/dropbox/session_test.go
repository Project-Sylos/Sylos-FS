package dropbox

import (
	"testing"

	"codeberg.org/Sylos/Sylos-FS/pkg/cloud"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

func TestForbiddenMigrationRootIDs(t *testing.T) {
	ids := (factory{}).ForbiddenMigrationRootIDs()
	if len(ids) != 1 || ids[0] != "teamSpace" {
		t.Fatalf("ForbiddenMigrationRootIDs=%v want [teamSpace]", ids)
	}
}

func TestParseDropboxContext_structuralSharedMount(t *testing.T) {
	ctx := parseDropboxContext(types.Folder{
		ServiceID: "14689517939",
		ParentId:  "14689517939",
		Type:      types.NodeTypeFolder,
	})
	if ctx.RootType != cloud.RootTypeTeamFolder {
		t.Fatalf("RootType=%q want team_folder", ctx.RootType)
	}
	if ctx.NamespaceID != "14689517939" {
		t.Fatalf("NamespaceID=%q", ctx.NamespaceID)
	}
	if ctx.FolderRef != "" {
		t.Fatalf("FolderRef=%q want empty (namespace root)", ctx.FolderRef)
	}
}

func TestParseDropboxContext_explicitTeamFolderType(t *testing.T) {
	ctx := parseDropboxContext(types.Folder{
		ServiceID: "14689517939",
		ParentId:  "14689517939",
		Type:      cloud.RootTypeTeamFolder,
	})
	if ctx.RootType != cloud.RootTypeTeamFolder || ctx.NamespaceID != "14689517939" {
		t.Fatalf("got %+v", ctx)
	}
}
