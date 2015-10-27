package vm_pool

import (
	"fmt"
	"strings"
	"time"

	"database/sql"
	"database/sql/driver"

	boshretry "github.com/cloudfoundry/bosh-utils/retrystrategy"
	sqlite3 "github.com/mattn/go-sqlite3"
	clock "github.com/pivotal-golang/clock"

	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
)

type DB interface {
	Begin() (*sql.Tx, error)
	Close() error
	Driver() driver.Driver
	Exec(query string, args ...interface{}) (sql.Result, error)
	Ping() error
	Prepare(query string) (*sql.Stmt, error)
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
	SetMaxIdleConns(n int)
	SetMaxOpenConns(n int)
}

type vmProperties struct {
	Id      int
	Name    string
	InUse   string
	ImageId string
	AgentId string
}

type VMInfoDB struct {
	db     DB
	logger boshlog.Logger

	VmProperties vmProperties
}

func NewVMInfoDB(id int, name string, in_use string, image_id string, agent_id string, logger boshlog.Logger, db DB) VMInfoDB {
	return VMInfoDB{
		VmProperties: vmProperties{
			Id:      id,
			Name:    name,
			InUse:   in_use,
			ImageId: image_id,
			AgentId: agent_id},
		db:     db,
		logger: logger,
	}
}

func (vmInfoDB *VMInfoDB) CloseDB() error {
	err := vmInfoDB.db.Close()
	if err != nil {
		return bosherr.WrapError(err, "Failed to close VM Pool DB connection")
	}
	return nil
}

func (vmInfoDB *VMInfoDB) QueryVMInfobyAgentID(retryTimeout time.Duration, retryInterval time.Duration) error {

	execStmtRetryable := boshretry.NewRetryable(
		func() (bool, error) {
			tx, err := vmInfoDB.db.Begin()
			if err != nil {
				sqliteErr := err.(sqlite3.Error)
				if sqliteErr.Code == sqlite3.ErrBusy || sqliteErr.Code == sqlite3.ErrLocked {
					return true, bosherr.WrapError(sqliteErr, "retrying...")
				} else {
					return false, bosherr.WrapError(sqliteErr, "Failed to begin DB transcation")
				}
			}

			var prepareStmt string
			if vmInfoDB.VmProperties.InUse == "t" {
				prepareStmt = "select id, image_id, agent_id from vms where in_use='t' and agent_id=?"
			} else if vmInfoDB.VmProperties.InUse == "f" {
				prepareStmt = "select id, image_id, agent_id from vms where in_use='f' and agent_id=?"
			} else {
				prepareStmt = "select id, image_id, agent_id from vms where agent_id==?"
			}

			sqlStmt, err := tx.Prepare(prepareStmt)
			defer sqlStmt.Close()
			if err != nil {
				sqliteErr := err.(sqlite3.Error)
				if sqliteErr.Code == sqlite3.ErrBusy || sqliteErr.Code == sqlite3.ErrLocked {
					return true, bosherr.WrapError(sqliteErr, "retrying...")
				} else {
					return false, bosherr.WrapError(sqliteErr, "Failed to prepare sql statement")
				}
			}

			err = sqlStmt.QueryRow(vmInfoDB.VmProperties.AgentId).Scan(&vmInfoDB.VmProperties.Id, &vmInfoDB.VmProperties.ImageId, &vmInfoDB.VmProperties.AgentId)
			if err != nil && !strings.Contains(err.Error(), "no rows") {
				sqliteErr := err.(sqlite3.Error)
				if sqliteErr.Code == sqlite3.ErrBusy || sqliteErr.Code == sqlite3.ErrLocked {
					fmt.Println("DB is busy or locked")
					return true, bosherr.WrapError(sqliteErr, "retrying...")
				} else {
					return false, bosherr.WrapError(sqliteErr, "Failed to query VM info from vms table")
				}

			}
			tx.Commit()
			return false, nil
		})

	timeService := clock.NewClock()
	timeoutRetryStrategy := boshretry.NewTimeoutRetryStrategy(retryTimeout, retryInterval, execStmtRetryable, timeService, vmInfoDB.logger)
	err := timeoutRetryStrategy.Try()
	if err != nil {
		return bosherr.WrapError(err, fmt.Sprintf("Failed to run QueryVMInfobyAgentID"))
	} else {
		return nil
	}

}

func (vmInfoDB *VMInfoDB) QueryVMInfobyID(retryTimeout time.Duration, retryInterval time.Duration) error {

	execStmtRetryable := boshretry.NewRetryable(
		func() (bool, error) {
			tx, err := vmInfoDB.db.Begin()
			if err != nil {
				sqliteErr := err.(sqlite3.Error)
				if sqliteErr.Code == sqlite3.ErrBusy || sqliteErr.Code == sqlite3.ErrLocked {
					return true, bosherr.WrapError(sqliteErr, "retrying...")
				} else {
					return false, bosherr.WrapError(sqliteErr, "Failed to begin DB transcation")
				}
			}

			var prepareStmt string
			if vmInfoDB.VmProperties.InUse == "t" {
				prepareStmt = "select id, in_use, image_id, agent_id from vms where id=? and in_use='t'"
			} else if vmInfoDB.VmProperties.InUse == "f" {
				prepareStmt = "select id, in_use, image_id, agent_id from vms where id=? and in_use='f'"
			} else {
				prepareStmt = "select id, in_use, image_id, agent_id from vms where id=?"
			}

			sqlStmt, err := tx.Prepare(prepareStmt)
			if err != nil {
				sqliteErr := err.(sqlite3.Error)
				if sqliteErr.Code == sqlite3.ErrBusy || sqliteErr.Code == sqlite3.ErrLocked {
					return true, bosherr.WrapError(sqliteErr, "retrying...")
				} else {
					return false, bosherr.WrapError(sqliteErr, "Failed to prepare sql statement")
				}
			}
			defer sqlStmt.Close()

			err = sqlStmt.QueryRow(vmInfoDB.VmProperties.Id).Scan(&vmInfoDB.VmProperties.Id, &vmInfoDB.VmProperties.InUse, &vmInfoDB.VmProperties.ImageId, &vmInfoDB.VmProperties.AgentId)
			if err != nil && !strings.Contains(err.Error(), "no rows") {
				sqliteErr := err.(sqlite3.Error)
				if sqliteErr.Code == sqlite3.ErrBusy || sqliteErr.Code == sqlite3.ErrLocked {
					fmt.Println("DB is busy or locked")
					return true, bosherr.WrapError(sqliteErr, "retrying...")
				} else {
					return false, bosherr.WrapError(sqliteErr, "Failed to query VM info from vms table")
				}

			}
			tx.Commit()
			return false, nil
		})

	timeService := clock.NewClock()
	timeoutRetryStrategy := boshretry.NewTimeoutRetryStrategy(retryTimeout, retryInterval, execStmtRetryable, timeService, vmInfoDB.logger)
	err := timeoutRetryStrategy.Try()
	if err != nil {
		return bosherr.WrapError(err, fmt.Sprintf("Failed to run QueryVMInfobyID"))
	} else {
		return nil
	}

}

func (vmInfoDB *VMInfoDB) DeleteVMFromVMDB(retryTimeout time.Duration, retryInterval time.Duration) error {
	sqlStmt := fmt.Sprintf("delete from vms where id=%d", vmInfoDB.VmProperties.Id)
	err := exec(vmInfoDB.db, sqlStmt, retryTimeout, retryInterval, vmInfoDB.logger)
	if err != nil {
		return bosherr.WrapError(err, "Failed to delete VM info from vms table")
	}
	return nil
}

func (vmInfoDB *VMInfoDB) InsertVMInfo(retryTimeout time.Duration, retryInterval time.Duration) error {
	sqlStmt := fmt.Sprintf("insert into vms (id, name, in_use, image_id, agent_id, timestamp) values (%d, '%s', '%s', '%s', '%s', CURRENT_TIMESTAMP)", vmInfoDB.VmProperties.Id, vmInfoDB.VmProperties.Name, vmInfoDB.VmProperties.InUse, vmInfoDB.VmProperties.ImageId, vmInfoDB.VmProperties.AgentId)
	//fmt.Println("sql statement: " + sqlStmt)
	err := exec(vmInfoDB.db, sqlStmt, retryTimeout, retryInterval, vmInfoDB.logger)
	if err != nil {
		return bosherr.WrapError(err, "Failed to insert VM info into vms table")
	}

	return nil
}

func (vmInfoDB *VMInfoDB) UpdateVMInfoByID(retryTimeout time.Duration, retryInterval time.Duration) error {

	execStmtRetryable := boshretry.NewRetryable(
		func() (bool, error) {
			tx, err := vmInfoDB.db.Begin()
			if err != nil {
				sqliteErr := err.(sqlite3.Error)
				if sqliteErr.Code == sqlite3.ErrBusy || sqliteErr.Code == sqlite3.ErrLocked {
					return true, bosherr.WrapError(sqliteErr, "retrying...")
				} else {
					return false, bosherr.WrapError(sqliteErr, "Failed to begin DB transcation")
				}
			}

			if vmInfoDB.VmProperties.InUse == "f" || vmInfoDB.VmProperties.InUse == "t" {
				sqlStmt := fmt.Sprintf("update vms set in_use='%s', timestamp=CURRENT_TIMESTAMP where id = %d", vmInfoDB.VmProperties.InUse, vmInfoDB.VmProperties.Id)
				fmt.Println("sql statement: " + sqlStmt)
				_, err = tx.Exec(sqlStmt)
				if err != nil {
					sqliteErr := err.(sqlite3.Error)
					if sqliteErr.Code == sqlite3.ErrBusy || sqliteErr.Code == sqlite3.ErrLocked {
						fmt.Println("DB is busy or locked")
						return true, bosherr.WrapError(sqliteErr, "retrying...")
					} else {
						return false, bosherr.WrapError(sqliteErr, "Failed to update in_use column in vms")
					}
				}
			}

			if vmInfoDB.VmProperties.ImageId != "" {
				sqlStmt := fmt.Sprintf("update vms set image_id='%s' where id = %d", vmInfoDB.VmProperties.ImageId, vmInfoDB.VmProperties.Id)
				_, err = tx.Exec(sqlStmt)
				if err != nil {
					sqliteErr := err.(sqlite3.Error)
					if sqliteErr.Code == sqlite3.ErrBusy || sqliteErr.Code == sqlite3.ErrLocked {
						return true, bosherr.WrapError(sqliteErr, "retrying...")
					} else {
						return false, bosherr.WrapError(sqliteErr, "Failed to update in_use column in vms")
					}
				}
			}

			if vmInfoDB.VmProperties.AgentId != "" {
				sqlStmt := fmt.Sprintf("update vms set agent_id='%s' where id = %d", vmInfoDB.VmProperties.AgentId, vmInfoDB.VmProperties.Id)
				_, err = tx.Exec(sqlStmt)
				if err != nil {
					sqliteErr := err.(sqlite3.Error)
					if sqliteErr.Code == sqlite3.ErrBusy || sqliteErr.Code == sqlite3.ErrLocked {
						fmt.Println("DB is busy or locked")
						return true, bosherr.WrapError(sqliteErr, "retrying...")
					} else {
						return false, bosherr.WrapError(sqliteErr, "Failed to update in_use column in vms")
					}
				}
			}
			tx.Commit()
			return false, nil
		})

	timeService := clock.NewClock()
	timeoutRetryStrategy := boshretry.NewTimeoutRetryStrategy(retryTimeout, retryInterval, execStmtRetryable, timeService, vmInfoDB.logger)
	err := timeoutRetryStrategy.Try()
	if err != nil {
		return bosherr.WrapError(err, fmt.Sprintf("Failed to run UpdateVMInfoByID"))
	} else {
		return nil
	}
}

// Private methods

func exec(db DB, sqlStmt string, retryTimeout time.Duration, retryInterval time.Duration, logger boshlog.Logger) error {

	execStmtRetryable := boshretry.NewRetryable(
		func() (bool, error) {
			tx, err := db.Begin()
			if err != nil {
				sqliteErr := err.(sqlite3.Error)
				if sqliteErr.Code == sqlite3.ErrBusy || sqliteErr.Code == sqlite3.ErrLocked {
					fmt.Println("err is " + err.Error())
					return true, bosherr.WrapError(sqliteErr, "Retrying...")
				} else {
					return false, bosherr.WrapError(sqliteErr, "Failed to begin DB transcation")
				}
			}

			_, err = tx.Exec(sqlStmt)
			if err != nil {
				sqliteErr := err.(sqlite3.Error)
				if sqliteErr.Code == sqlite3.ErrBusy || sqliteErr.Code == sqlite3.ErrLocked {
					fmt.Println("DB is busy or locked")
					return true, bosherr.WrapError(sqliteErr, "Retrying...")
				} else {
					return false, bosherr.WrapError(sqliteErr, "Failed to execute sql statement: "+sqlStmt)
				}
			}

			tx.Commit()
			return false, nil
		})

	timeService := clock.NewClock()
	timeoutRetryStrategy := boshretry.NewTimeoutRetryStrategy(retryTimeout, retryInterval, execStmtRetryable, timeService, logger)
	err := timeoutRetryStrategy.Try()
	if err != nil {
		return bosherr.WrapError(err, fmt.Sprintf("Failed to execute the sql statment %s", sqlStmt))
	} else {
		return nil
	}

}
