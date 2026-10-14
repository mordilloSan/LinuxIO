package storage

import (
	"slices"
	"strings"
	"testing"
)

const btrfsMaintenanceFixture = `## Type: string(none,weekly,monthly)
# Frequency of periodic scrub.
BTRFS_SCRUB_PERIOD="monthly"
BTRFS_SCRUB_MOUNTPOINTS="/"
BTRFS_BALANCE_PERIOD="none"
BTRFS_BALANCE_MOUNTPOINTS="/"
BTRFS_TRIM_PERIOD="none"
BTRFS_TRIM_MOUNTPOINTS="auto"
`

func scrubTask(t *testing.T) btrfsMaintenanceTask {
	t.Helper()
	for _, task := range btrfsMaintenanceTasks {
		if task.Name == "scrub" {
			return task
		}
	}
	t.Fatal("scrub task missing")
	return btrfsMaintenanceTask{}
}

func TestBtrfsMaintenanceScheduleReadsConfig(t *testing.T) {
	schedule := btrfsMaintenanceSchedule(btrfsMaintenanceFixture, []string{"/", "/data"})
	if len(schedule.Tasks) != 4 {
		t.Fatalf("tasks = %d, want 4", len(schedule.Tasks))
	}
	scrub, balance, trim, defrag := schedule.Tasks[0], schedule.Tasks[1], schedule.Tasks[2], schedule.Tasks[3]
	if scrub.Period != "monthly" || !slices.Equal(scrub.Mountpoints, []string{"/"}) {
		t.Errorf("scrub = %+v", scrub)
	}
	// A disabled task targets nothing, even though the Debian default lists "/".
	if balance.Period != "none" || len(balance.Mountpoints) != 0 {
		t.Errorf("balance = %+v", balance)
	}
	if trim.Period != "none" || len(trim.Mountpoints) != 0 {
		t.Errorf("trim = %+v", trim)
	}
	// Missing variables read as off; the allowed periods still come through.
	if defrag.Period != "none" || !slices.Equal(defrag.Periods, []string{"none", "daily", "weekly", "monthly"}) {
		t.Errorf("defrag = %+v", defrag)
	}
}

func TestBtrfsMaintenanceScheduleExpandsAuto(t *testing.T) {
	content := strings.Replace(btrfsMaintenanceFixture, `BTRFS_TRIM_PERIOD="none"`, `BTRFS_TRIM_PERIOD="weekly"`, 1)
	schedule := btrfsMaintenanceSchedule(content, []string{"/", "/data"})
	if trim := schedule.Tasks[2]; !slices.Equal(trim.Mountpoints, []string{"/", "/data"}) {
		t.Errorf("trim mountpoints = %v, want auto expanded", trim.Mountpoints)
	}
}

func TestApplyBtrfsMaintenanceEnablesAndSharesPeriod(t *testing.T) {
	updated, err := applyBtrfsMaintenance(btrfsMaintenanceFixture, scrubTask(t), "/data", "weekly", nil)
	if err != nil {
		t.Fatal(err)
	}
	vars := parseShellVars(updated)
	if vars["BTRFS_SCRUB_PERIOD"] != "weekly" || vars["BTRFS_SCRUB_MOUNTPOINTS"] != "/:/data" {
		t.Errorf("vars = %v", vars)
	}
	if !strings.Contains(updated, "# Frequency of periodic scrub.") {
		t.Error("comments were not preserved")
	}
	if strings.Count(updated, "BTRFS_SCRUB_PERIOD=") != 1 {
		t.Error("period assignment duplicated")
	}
}

func TestApplyBtrfsMaintenanceDisablesOnlyWhenListEmpties(t *testing.T) {
	content := strings.Replace(btrfsMaintenanceFixture, `BTRFS_SCRUB_MOUNTPOINTS="/"`, `BTRFS_SCRUB_MOUNTPOINTS="/:/data"`, 1)
	updated, err := applyBtrfsMaintenance(content, scrubTask(t), "/data", "none", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vars := parseShellVars(updated); vars["BTRFS_SCRUB_PERIOD"] != "monthly" || vars["BTRFS_SCRUB_MOUNTPOINTS"] != "/" {
		t.Errorf("after removing /data: %v", vars)
	}
	updated, err = applyBtrfsMaintenance(updated, scrubTask(t), "/", "none", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vars := parseShellVars(updated); vars["BTRFS_SCRUB_PERIOD"] != "none" || vars["BTRFS_SCRUB_MOUNTPOINTS"] != "" {
		t.Errorf("after removing /: %v", vars)
	}
}

func TestApplyBtrfsMaintenanceStartsFreshFromDisabledDefault(t *testing.T) {
	balance := btrfsMaintenanceTasks[1]
	updated, err := applyBtrfsMaintenance(btrfsMaintenanceFixture, balance, "/data", "monthly", nil)
	if err != nil {
		t.Fatal(err)
	}
	// The default "/" of a disabled task must not be balanced as a side effect.
	if vars := parseShellVars(updated); vars["BTRFS_BALANCE_MOUNTPOINTS"] != "/data" || vars["BTRFS_BALANCE_PERIOD"] != "monthly" {
		t.Errorf("vars = %v", vars)
	}
}

func TestApplyBtrfsMaintenanceAppendsMissingVariablesAndRejectsBadPeriod(t *testing.T) {
	defrag := btrfsMaintenanceTasks[3]
	updated, err := applyBtrfsMaintenance("", defrag, "/data", "daily", nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := "BTRFS_DEFRAG_PERIOD=\"daily\"\nBTRFS_DEFRAG_PATHS=\"/data\"\n"; updated != want {
		t.Errorf("updated = %q, want %q", updated, want)
	}
	if _, err := applyBtrfsMaintenance("", scrubTask(t), "/data", "daily", nil); err == nil {
		t.Error("daily scrub should be rejected")
	}
}
