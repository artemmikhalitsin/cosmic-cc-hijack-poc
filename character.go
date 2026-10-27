package main

import (
	"fmt"
	"strings"
)

type characterStats struct {
	ID    int32
	Name  string
	Level byte
	Job   uint16
	HP    uint16
	MaxHP uint16
	MP    uint16
	MaxMP uint16
	EXP   uint32
	MapID int32
	Mesos int32
}

type characterEntry struct {
	ID    int32
	Name  string
	Level byte
}

type spawnEvent struct {
	ID    int32
	Name  string
	Level byte
}

func hasSPTable(job uint16) bool {
	if job == 2001 || job == 2200 || job == 2218 {
		return true
	}
	return job >= 2210 && job <= 2217
}

func readCharacterStats(r *packetReader) characterStats {
	var stats characterStats
	stats.ID = r.i32()
	stats.Name = r.fixedString(13)
	r.skip(2)
	r.i32()
	r.i32()
	r.skip(24)
	stats.Level = r.u8()
	stats.Job = r.u16()
	r.skip(8)
	stats.HP = r.u16()
	stats.MaxHP = r.u16()
	stats.MP = r.u16()
	stats.MaxMP = r.u16()
	r.skip(2)
	if hasSPTable(stats.Job) {
		count := int(r.u8())
		r.skip(count * 2)
	} else {
		r.skip(2)
	}
	stats.EXP = r.u32()
	r.skip(6)
	stats.MapID = r.i32()
	r.skip(5)
	return stats
}

func skipCharacterLook(r *packetReader) {
	r.skip(11)
	for list := 0; list < 2; list++ {
		for {
			position := r.u8()
			if position == 0xFF || r.err != nil {
				break
			}
			r.skip(4)
		}
	}
	r.skip(16)
}

func parseCharacterList(body []byte) ([]characterEntry, error) {
	r := newPacketReader(body)
	if r.u16() != 0x000B {
		return nil, fmt.Errorf("not a character list packet")
	}
	if status := r.u8(); status != 0 {
		return nil, fmt.Errorf("character list failed with status %d", status)
	}
	count := int(r.u8())
	entries := make([]characterEntry, 0, count)
	for i := 0; i < count; i++ {
		stats := readCharacterStats(r)
		skipCharacterLook(r)
		r.u8()
		if r.u8() == 1 {
			r.skip(16)
		}
		entries = append(entries, characterEntry{ID: stats.ID, Name: stats.Name, Level: stats.Level})
	}
	if r.err != nil {
		return nil, fmt.Errorf("parse character list: %w", r.err)
	}
	return entries, nil
}

func findCharacter(entries []characterEntry, name string) (characterEntry, error) {
	for _, entry := range entries {
		if strings.EqualFold(entry.Name, name) {
			return entry, nil
		}
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, fmt.Sprintf("%s (%d)", entry.Name, entry.ID))
	}
	return characterEntry{}, fmt.Errorf("character %q not found; available: %s", name, strings.Join(names, ", "))
}

func parseSetField(body []byte) (characterStats, error) {
	r := newPacketReader(body)
	if r.u16() != sendSetField {
		return characterStats{}, fmt.Errorf("not a set-field packet")
	}
	r.skip(20)
	r.u64()
	r.u8()
	stats := readCharacterStats(r)
	r.u8()
	if r.u8() == 1 {
		r.string()
	}
	stats.Mesos = r.i32()
	if r.err != nil {
		return characterStats{}, fmt.Errorf("parse set-field: %w", r.err)
	}
	return stats, nil
}

func parseSpawnPlayer(body []byte) (spawnEvent, error) {
	r := newPacketReader(body)
	if r.u16() != sendSpawnPlayer {
		return spawnEvent{}, fmt.Errorf("not a player-spawn packet")
	}
	event := spawnEvent{ID: r.i32(), Level: r.u8(), Name: r.string()}
	if r.err != nil {
		return spawnEvent{}, r.err
	}
	return event, nil
}

func parseRemovePlayer(body []byte) (int32, error) {
	r := newPacketReader(body)
	if r.u16() != sendRemovePlayer {
		return 0, fmt.Errorf("not a player-removal packet")
	}
	id := r.i32()
	return id, r.err
}

func (s characterStats) String() string {
	return fmt.Sprintf("id=%d name=%s level=%d job=%d hp=%d/%d mp=%d/%d exp=%d mesos=%d map=%d",
		s.ID, s.Name, s.Level, s.Job, s.HP, s.MaxHP, s.MP, s.MaxMP, s.EXP, s.Mesos, s.MapID)
}
