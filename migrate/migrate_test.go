package migrate_test

import (
	"fmt"
	"github.com/JackC/pgx"
	"github.com/JackC/pgx/migrate"
	. "gopkg.in/check.v1"
	"testing"
)

type MigrateSuite struct {
	conn *pgx.Connection
}

func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&MigrateSuite{})

var versionTable string = "schema_version"

func (s *MigrateSuite) SetUpTest(c *C) {
	var err error
	s.conn, err = pgx.Connect(*defaultConnectionParameters)
	c.Assert(err, IsNil)

	s.cleanupSampleMigrator(c)
}

func (s *MigrateSuite) SelectValue(c *C, sql string, arguments ...interface{}) interface{} {
	value, err := s.conn.SelectValue(sql, arguments...)
	c.Assert(err, IsNil)
	return value
}

func (s *MigrateSuite) Execute(c *C, sql string, arguments ...interface{}) string {
	commandTag, err := s.conn.Execute(sql, arguments...)
	c.Assert(err, IsNil)
	return commandTag
}

func (s *MigrateSuite) tableExists(c *C, tableName string) bool {
	return s.SelectValue(c,
		"select exists(select 1 from information_schema.tables where table_catalog=$1 and table_name=$2)",
		defaultConnectionParameters.Database,
		tableName).(bool)
}

func (s *MigrateSuite) createEmptyMigrator(c *C) *migrate.Migrator {
	var err error
	m, err := migrate.NewMigrator(s.conn, versionTable)
	c.Assert(err, IsNil)
	return m
}

func (s *MigrateSuite) createSampleMigrator(c *C) *migrate.Migrator {
	m := s.createEmptyMigrator(c)
	m.AppendMigration("Create t1", "create table t1(id serial);", "drop table t1;")
	m.AppendMigration("Create t2", "create table t2(id serial);", "drop table t2;")
	m.AppendMigration("Create t3", "create table t3(id serial);", "drop table t3;")
	return m
}

func (s *MigrateSuite) cleanupSampleMigrator(c *C) {
	tables := []string{versionTable, "t1", "t2", "t3"}
	for _, table := range tables {
		s.Execute(c, "drop table if exists "+table)
	}
}

func (s *MigrateSuite) TestNewMigrator(c *C) {
	var m *migrate.Migrator
	var err error

	// Initial run
	m, err = migrate.NewMigrator(s.conn, versionTable)
	c.Assert(err, IsNil)

	// Creates version table
	schemaVersionExists := s.tableExists(c, versionTable)
	c.Assert(schemaVersionExists, Equals, true)

	// Succeeds when version table is already created
	m, err = migrate.NewMigrator(s.conn, versionTable)
	c.Assert(err, IsNil)

	initialVersion, err := m.GetCurrentVersion()
	c.Assert(err, IsNil)
	c.Assert(initialVersion, Equals, int32(0))
}

func (s *MigrateSuite) TestAppendMigration(c *C) {
	m := s.createEmptyMigrator(c)

	name := "Create t"
	upSQL := "create t..."
	downSQL := "drop t..."
	m.AppendMigration(name, upSQL, downSQL)

	c.Assert(len(m.Migrations), Equals, 1)
	c.Assert(m.Migrations[0].Name, Equals, name)
	c.Assert(m.Migrations[0].UpSQL, Equals, upSQL)
	c.Assert(m.Migrations[0].DownSQL, Equals, downSQL)
}

func (s *MigrateSuite) TestMigrate(c *C) {
	m := s.createSampleMigrator(c)

	err := m.Migrate()
	c.Assert(err, IsNil)
	currentVersion := s.SelectValue(c, "select version from schema_version")
	c.Assert(currentVersion, Equals, int32(3))
}

func (s *MigrateSuite) TestMigrateTo(c *C) {
	m := s.createSampleMigrator(c)

	var onStartCallUpCount int
	var onStartCallDownCount int
	m.OnStart = func(_ *migrate.Migration, direction string) {
		switch direction {
		case "up":
			onStartCallUpCount++
		case "down":
			onStartCallDownCount++
		default:
			c.Fatalf("Unexpected direction: %s", direction)
		}
	}

	// Migrate to -1 is error
	err := m.MigrateTo(-1)
	c.Assert(err, ErrorMatches, "schema_version version -1 is outside the valid versions of 0 to 3")

	// Migrate past end is error
	err = m.MigrateTo(int32(len(m.Migrations)) + 1)
	c.Assert(err, ErrorMatches, "schema_version version 4 is outside the valid versions of 0 to 3")

	// Migrate from 0 up to 1
	err = m.MigrateTo(1)
	c.Assert(err, IsNil)
	currentVersion := s.SelectValue(c, "select version from schema_version")
	c.Assert(currentVersion, Equals, int32(1))
	c.Assert(s.tableExists(c, "t1"), Equals, true)
	c.Assert(s.tableExists(c, "t2"), Equals, false)
	c.Assert(s.tableExists(c, "t3"), Equals, false)
	c.Assert(onStartCallUpCount, Equals, 1)
	c.Assert(onStartCallDownCount, Equals, 0)

	// Migrate from 1 up to 3
	err = m.MigrateTo(3)
	c.Assert(err, IsNil)
	currentVersion = s.SelectValue(c, "select version from schema_version")
	c.Assert(currentVersion, Equals, int32(3))
	c.Assert(s.tableExists(c, "t1"), Equals, true)
	c.Assert(s.tableExists(c, "t2"), Equals, true)
	c.Assert(s.tableExists(c, "t3"), Equals, true)
	c.Assert(onStartCallUpCount, Equals, 3)
	c.Assert(onStartCallDownCount, Equals, 0)

	// Migrate from 3 to 3 is no-op
	err = m.MigrateTo(3)
	c.Assert(err, IsNil)
	currentVersion = s.SelectValue(c, "select version from schema_version")
	c.Assert(currentVersion, Equals, int32(3))
	c.Assert(s.tableExists(c, "t1"), Equals, true)
	c.Assert(s.tableExists(c, "t2"), Equals, true)
	c.Assert(s.tableExists(c, "t3"), Equals, true)
	c.Assert(onStartCallUpCount, Equals, 3)
	c.Assert(onStartCallDownCount, Equals, 0)

	// Migrate from 3 down to 1
	err = m.MigrateTo(1)
	c.Assert(err, IsNil)
	currentVersion = s.SelectValue(c, "select version from schema_version")
	c.Assert(currentVersion, Equals, int32(1))
	c.Assert(s.tableExists(c, "t1"), Equals, true)
	c.Assert(s.tableExists(c, "t2"), Equals, false)
	c.Assert(s.tableExists(c, "t3"), Equals, false)
	c.Assert(onStartCallUpCount, Equals, 3)
	c.Assert(onStartCallDownCount, Equals, 2)

	// Migrate from 1 down to 0
	err = m.MigrateTo(0)
	c.Assert(err, IsNil)
	currentVersion = s.SelectValue(c, "select version from schema_version")
	c.Assert(currentVersion, Equals, int32(0))
	c.Assert(s.tableExists(c, "t1"), Equals, false)
	c.Assert(s.tableExists(c, "t2"), Equals, false)
	c.Assert(s.tableExists(c, "t3"), Equals, false)
	c.Assert(onStartCallUpCount, Equals, 3)
	c.Assert(onStartCallDownCount, Equals, 3)

	// Migrate back up to 3
	err = m.MigrateTo(3)
	c.Assert(err, IsNil)
	currentVersion = s.SelectValue(c, "select version from schema_version")
	c.Assert(currentVersion, Equals, int32(3))
	c.Assert(s.tableExists(c, "t1"), Equals, true)
	c.Assert(s.tableExists(c, "t2"), Equals, true)
	c.Assert(s.tableExists(c, "t3"), Equals, true)
	c.Assert(onStartCallUpCount, Equals, 6)
	c.Assert(onStartCallDownCount, Equals, 3)
}

func Example_OnStartMigrationProgressLogging() {
	conn, err := pgx.Connect(*defaultConnectionParameters)
	if err != nil {
		fmt.Printf("Unable to establish connection: %v", err)
		return
	}

	// Clear any previous runs
	if _, err = conn.Execute("drop table if exists schema_version"); err != nil {
		fmt.Printf("Unable to drop schema_version table: %v", err)
		return
	}

	var m *migrate.Migrator
	m, err = migrate.NewMigrator(conn, "schema_version")
	if err != nil {
		fmt.Printf("Unable to create migrator: %v", err)
		return
	}

	m.OnStart = func(migration *migrate.Migration, direction string) {
		fmt.Printf("Migrating %s: %s", direction, migration.Name)
	}

	m.AppendMigration("create a table", "create temporary table foo(id serial primary key)", "")

	if err = m.Migrate(); err != nil {
		fmt.Printf("Unexpected failure migrating: %v", err)
		return
	}
	// Output:
	// Migrating up: create a table
}