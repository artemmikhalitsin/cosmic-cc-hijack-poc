package main

import (
	"testing"
)

func writeTestStats(w *packetWriter, job uint16) {
	w.i32(1001)
	name := []byte("Victim")
	w.raw(name)
	w.raw(make([]byte, 13-len(name)))
	w.u8(0)
	w.u8(0)
	w.i32(20000)
	w.i32(30000)
	w.raw(make([]byte, 24))
	w.u8(73)
	w.u16(job)
	w.u16(100)
	w.u16(200)
	w.u16(300)
	w.u16(400)
	w.u16(1234)
	w.u16(2345)
	w.u16(567)
	w.u16(890)
	w.u16(12)
	if hasSPTable(job) {
		w.u8(1)
		w.u8(2)
		w.u8(9)
	} else {
		w.u16(34)
	}
	w.u32(456789)
	w.u16(25)
	w.u32(100)
	w.i32(100000000)
	w.u8(3)
	w.u32(0)
}

func finishTestLook(w *packetWriter) {
	w.raw(make([]byte, 11))
	w.u8(5)
	w.i32(1302000)
	w.u8(0xFF)
	w.u8(0xFF)
	w.i32(0)
	w.i32(0)
	w.i32(0)
	w.i32(0)
}

func TestParseCharacterList(t *testing.T) {
	w := packetWithOpcode(0x000B)
	w.u8(0)
	w.u8(1)
	writeTestStats(w, 400)
	finishTestLook(w)
	w.u8(0)
	w.u8(1)
	w.u32(1)
	w.u32(2)
	w.u32(3)
	w.u32(4)
	w.u8(2)
	w.u32(8)

	entries, err := parseCharacterList(w.bytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != 1001 || entries[0].Name != "Victim" || entries[0].Level != 73 {
		t.Fatalf("unexpected entries: %+v", entries)
	}
	selected, err := findCharacter(entries, "victim")
	if err != nil || selected.ID != 1001 {
		t.Fatalf("selection failed: %+v, %v", selected, err)
	}
}

func TestParseSetFieldEvidence(t *testing.T) {
	for _, job := range []uint16{400, 2001, 2210, 2218} {
		w := packetWithOpcode(sendSetField)
		w.raw(make([]byte, 20))
		w.u64(^uint64(0))
		w.u8(0)
		writeTestStats(w, job)
		w.u8(20)
		w.u8(0)
		w.i32(7654321)
		w.raw(make([]byte, 20))

		stats, err := parseSetField(w.bytes)
		if err != nil {
			t.Fatalf("job %d: %v", job, err)
		}
		if stats.ID != 1001 || stats.Name != "Victim" || stats.Level != 73 || stats.Job != job {
			t.Fatalf("job %d: wrong identity: %+v", job, stats)
		}
		if stats.HP != 1234 || stats.MaxHP != 2345 || stats.MP != 567 || stats.MaxMP != 890 {
			t.Fatalf("job %d: wrong HP/MP: %+v", job, stats)
		}
		if stats.EXP != 456789 || stats.Mesos != 7654321 || stats.MapID != 100000000 {
			t.Fatalf("job %d: wrong private stats: %+v", job, stats)
		}
	}
}

func TestParseSpawnAndRemoval(t *testing.T) {
	spawn := packetWithOpcode(sendSpawnPlayer)
	spawn.i32(1001)
	spawn.u8(73)
	spawn.string("Victim")
	event, err := parseSpawnPlayer(spawn.bytes)
	if err != nil || event.ID != 1001 || event.Name != "Victim" || event.Level != 73 {
		t.Fatalf("unexpected spawn: %+v, %v", event, err)
	}

	removal := packetWithOpcode(sendRemovePlayer)
	removal.i32(1001)
	id, err := parseRemovePlayer(removal.bytes)
	if err != nil || id != 1001 {
		t.Fatalf("unexpected removal: %d, %v", id, err)
	}
}
