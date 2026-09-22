package store

import (
	"context"
	"database/sql"

	"tga-review/internal/model"
)

// LatestRevision returns the current revision number for a kind (0 if none).
func (s *Store) LatestRevision(ctx context.Context, projectID string, kind model.SourceKind) (int, error) {
	var rev sql.NullInt64
	err := s.DB.QueryRowContext(ctx,
		`SELECT MAX(revision) FROM source_versions WHERE project_id=? AND kind=?`,
		projectID, string(kind)).Scan(&rev)
	if err != nil {
		return 0, err
	}
	return int(rev.Int64), nil
}

// LatestVersionID returns the active version id of a kind ("" if none).
func (s *Store) LatestVersionID(ctx context.Context, projectID string, kind model.SourceKind) (string, error) {
	var id string
	err := s.DB.QueryRowContext(ctx,
		`SELECT id FROM source_versions WHERE project_id=? AND kind=? AND active=1`,
		projectID, string(kind)).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

// InsertSourceVersion adds an immutable version. When parseOK it activates the
// version and deactivates its predecessors; a half-failed layer (parseOK=false)
// stays inactive and never shadows prior good data.
func (s *Store) InsertSourceVersion(ctx context.Context, doc model.RawDoc, v model.SourceVersion) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO source_versions
			 (id,project_id,kind,parent_id,batch_id,revision,created_at,parse_ok,diagnostic,active,raw,format,sha256)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			v.ID, v.ProjectID, string(v.Kind), v.ParentID, v.BatchID, v.Revision,
			v.CreatedAt.UTC().Format(timeFmt), boolI(v.ParseOK), v.Diagnostic, boolI(v.Active),
			doc.Raw, doc.Format, doc.SHA256); err != nil {
			return err
		}
		if v.Active && v.ParseOK {
			if _, err := tx.ExecContext(ctx,
				`UPDATE source_versions SET active=0 WHERE project_id=? AND kind=? AND id<>?`,
				v.ProjectID, string(v.Kind), v.ID); err != nil {
				return err
			}
		}
		return nil
	})
}

func boolI(b bool) int {
	if b {
		return 1
	}
	return 0
}

// InsertSamplePoints stores parsed sample rows for a version.
func (s *Store) InsertSamplePoints(ctx context.Context, pts []model.SamplePoint) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx,
			`INSERT INTO sample_points
			 (version_id,seq,time_raw,temp_raw,mass_raw,time_v,temp_v,mass_v,time_p,temp_p,mass_p,unit_time,unit_temp,unit_mass,row_note)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, p := range pts {
			if _, err := stmt.ExecContext(ctx, p.VersionID, p.Seq, p.TimeRaw, p.TempRaw, p.MassRaw,
				nf(p.TimeS), nf(p.TempS), nf(p.MassS), boolI(p.TimeP), boolI(p.TempP), boolI(p.MassP),
				p.UnitTime, p.UnitTemp, p.UnitMass, p.RowNote); err != nil {
				return err
			}
		}
		return nil
	})
}

// InsertGaps stores declared sampling gaps for a version.
func (s *Store) InsertGaps(ctx context.Context, gaps []model.Gap) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx,
			`INSERT INTO sample_gaps(version_id,seq,from_v,to_v,unit,raw) VALUES(?,?,?,?,?,?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, g := range gaps {
			if _, err := stmt.ExecContext(ctx, g.VersionID, g.Seq, g.FromS, g.ToS, g.Unit, g.Raw); err != nil {
				return err
			}
		}
		return nil
	})
}

// InsertDrift stores drift correction rows.
func (s *Store) InsertDrift(ctx context.Context, pts []model.DriftPoint) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx,
			`INSERT INTO drift_points(version_id,seq,time_raw,drift_raw,time_v,drift_v,time_p,drift_p,unit_time,unit_drift)
			 VALUES(?,?,?,?,?,?,?,?,?,?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, p := range pts {
			if _, err := stmt.ExecContext(ctx, p.VersionID, p.Seq, p.TimeRaw, p.DriftRaw,
				nf(p.TimeS), nf(p.DriftS), boolI(p.TimeP), boolI(p.DriftP), p.UnitTime, p.UnitDrift); err != nil {
				return err
			}
		}
		return nil
	})
}

// InsertFurnace stores furnace program segments.
func (s *Store) InsertFurnace(ctx context.Context, segs []model.FurnaceSeg) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx,
			`INSERT INTO furnace_segs(version_id,seq,time_raw,temp_raw,time_v,temp_v,time_p,temp_p,unit_time,unit_temp,seg_kind)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, p := range segs {
			if _, err := stmt.ExecContext(ctx, p.VersionID, p.Seq, p.TimeRaw, p.TempRaw,
				nf(p.TimeS), nf(p.TempS), boolI(p.TimeP), boolI(p.TempP), p.UnitTime, p.UnitTemp, p.Kind); err != nil {
				return err
			}
		}
		return nil
	})
}

// InsertGas stores gas valve events.
func (s *Store) InsertGas(ctx context.Context, evs []model.GasEvent) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx,
			`INSERT INTO gas_events(version_id,seq,time_raw,gas_raw,action_raw,time_v,time_p,unit_time,gas,action)
			 VALUES(?,?,?,?,?,?,?,?,?,?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, p := range evs {
			if _, err := stmt.ExecContext(ctx, p.VersionID, p.Seq, p.TimeRaw, p.GasRaw, p.ActionRaw,
				nf(p.TimeS), boolI(p.TimeP), p.UnitTime, p.Gas, p.Action); err != nil {
				return err
			}
		}
		return nil
	})
}

// InsertBlank stores blank curve points.
func (s *Store) InsertBlank(ctx context.Context, pts []model.BlankPoint) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx,
			`INSERT INTO blank_points(version_id,seq,time_raw,mass_raw,time_v,mass_v,time_p,mass_p,unit_time,unit_mass)
			 VALUES(?,?,?,?,?,?,?,?,?,?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, p := range pts {
			if _, err := stmt.ExecContext(ctx, p.VersionID, p.Seq, p.TimeRaw, p.MassRaw,
				nf(p.TimeS), nf(p.MassS), boolI(p.TimeP), boolI(p.MassP), p.UnitTime, p.UnitMass); err != nil {
				return err
			}
		}
		return nil
	})
}

// InsertInitial stores initial mass declarations.
func (s *Store) InsertInitial(ctx context.Context, ms []model.InitialMass) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx,
			`INSERT INTO initial_mass(version_id,seq,mass_raw,mass_v,mass_p,unit,basis)
			 VALUES(?,?,?,?,?,?,?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, p := range ms {
			if _, err := stmt.ExecContext(ctx, p.VersionID, p.Seq, p.MassRaw,
				nf(p.MassS), boolI(p.MassP), p.Unit, p.Basis); err != nil {
				return err
			}
		}
		return nil
	})
}

// InsertParams stores an algorithm parameter set.
func (s *Store) InsertParams(ctx context.Context, versionID string, vals map[string]string) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx,
			`INSERT INTO param_sets(version_id,key,value) VALUES(?,?,?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for k, v := range vals {
			if _, err := stmt.ExecContext(ctx, versionID, k, v); err != nil {
				return err
			}
		}
		return nil
	})
}
