# GORM ORM Research Summary

## 1. Model Definition

### gorm.Model
Embed `gorm.Model` to get four standard fields:
```go
type Model struct {
    ID        uint           `gorm:"primaryKey"`
    CreatedAt time.Time
    UpdatedAt time.Time
    DeletedAt gorm.DeletedAt `gorm:"index"`
}
```
- `ID` is auto-increment primary key
- `CreatedAt` / `UpdatedAt` are auto-managed timestamps
- `DeletedAt` enables soft delete (records are hidden, not removed)

### Key Struct Tags
- `column:name` - override column name
- `primaryKey` - mark as PK
- `unique` - unique constraint
- `not null` - NOT NULL constraint
- `default:value` - default value
- `index` / `uniqueIndex` - create indexes
- `size:256` - column size
- `type:varchar(100)` - exact column type
- `autoIncrement` - auto increment
- `<-:create` / `<-:update` / `<-` / `->` / `-` - field-level read/write permissions

### Embedded Structs
- Anonymous fields are auto-included
- Named struct fields need `gorm:"embedded"` tag
- Use `gorm:"embeddedPrefix:prefix_"` for column prefixes

### Zero-Value Handling
Struct-based queries/updates skip zero values. Use pointers (`*int`) or `sql.NullBool` / `sql.NullString` to distinguish zero from unset.

---

## 2. Naming Conventions

- **Table names**: Struct name -> pluralized snake_case (`UserProfile` -> `user_profiles`)
- **Column names**: Field name -> snake_case (`CreatedAt` -> `created_at`)
- **Primary key**: Field named `ID` is the default PK
- **Foreign keys**: OwnerTypeName + PrimaryFieldName (`UserID` for a User owner)
- Override table name: implement `TableName() string` on the model
- Override globally: set custom `NamingStrategy` in `gorm.Config`

---

## 3. SQLite-Specific Setup

### Driver & Connection
```go
import (
    "gorm.io/driver/sqlite"
    "gorm.io/gorm"
)

// File-based
db, err := gorm.Open(sqlite.Open("app.db"), &gorm.Config{})

// In-memory (for testing)
db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
```

### WAL Mode
SQLite WAL mode is set via PRAGMA after opening:
```go
sqlDB, _ := db.DB()
sqlDB.Exec("PRAGMA journal_mode=WAL;")
```

### Connection Pool for SQLite
```go
sqlDB, _ := db.DB()
sqlDB.SetMaxIdleConns(1)
sqlDB.SetMaxOpenConns(1)  // SQLite only supports one writer
sqlDB.SetConnMaxLifetime(time.Hour)
```

---

## 4. Relationship Modeling

### Belongs To (many-to-one)
```go
type User struct {
    gorm.Model
    Name      string
    CompanyID uint      // foreign key
    Company   Company   // association
}
type Company struct {
    gorm.Model
    Name string
}
```
Override FK: `gorm:"foreignKey:CompanyRefer"`
Override reference: `gorm:"references:Code"`
Constraints: `gorm:"constraint:OnUpdate:CASCADE,OnDelete:SET NULL;"`

### Has Many (one-to-many)
```go
type User struct {
    gorm.Model
    CreditCards []CreditCard
}
type CreditCard struct {
    gorm.Model
    Number string
    UserID uint  // convention-based FK
}
```
Override FK: `gorm:"foreignKey:UserRefer"`
Override reference: `gorm:"foreignKey:UserNumber;references:MemberNumber"`

### Association Operations
```go
// Find
db.Model(&user).Association("Languages").Find(&languages)

// Append
db.Model(&user).Association("Languages").Append(&newLang)

// Replace
db.Model(&user).Association("Languages").Replace(&langs)

// Delete (removes relationship, not record)
db.Model(&user).Association("Languages").Delete(&lang)

// Clear all
db.Model(&user).Association("Languages").Clear()

// Count
db.Model(&user).Association("Languages").Count()
```

### Auto Create/Update Associations
- By default, creating a parent also creates/upserts its associations
- Skip with: `db.Omit(clause.Associations).Create(&user)`
- Full update mode: `db.Session(&gorm.Session{FullSaveAssociations: true}).Updates(&user)`

---

## 5. CRUD Operations

### Create
```go
user := User{Name: "Alice", Age: 30}
result := db.Create(&user)  // pass pointer
// user.ID is populated after insert
// result.Error, result.RowsAffected

// Batch create
db.Create(&[]User{{Name: "A"}, {Name: "B"}})
db.CreateInBatches(&users, 100)  // in batches of 100

// Upsert
db.Clauses(clause.OnConflict{
    Columns:   []clause.Column{{Name: "id"}},
    DoUpdates: clause.AssignmentColumns([]string{"name", "age"}),
}).Create(&users)
```

### Read
```go
// Single record
db.First(&user)              // ORDER BY id LIMIT 1
db.First(&user, 10)          // WHERE id = 10
db.First(&user, "id = ?", "uuid-here")

// Multiple
db.Find(&users)
db.Where("age > ?", 18).Find(&users)
db.Where(&User{Name: "alice"}).Find(&users)  // struct condition (skips zero values!)

// Select specific fields
db.Select("name", "age").Find(&users)

// Pagination
db.Limit(10).Offset(20).Find(&users)

// Ordering
db.Order("created_at desc").Find(&users)
```

### Update
```go
// Single column
db.Model(&user).Update("name", "new_name")

// Multiple columns (struct - skips zero values)
db.Model(&user).Updates(User{Name: "new", Age: 0})  // Age NOT updated!

// Multiple columns (map - includes zero values)
db.Model(&user).Updates(map[string]interface{}{"name": "new", "age": 0})

// Save all fields (upsert)
db.Save(&user)

// SQL expression
db.Model(&product).Update("price", gorm.Expr("price * ? + ?", 2, 100))
```

### Delete
```go
// Soft delete (if DeletedAt field exists)
db.Delete(&user, 10)  // sets DeletedAt, doesn't remove

// Hard delete
db.Unscoped().Delete(&user, 10)  // permanent removal

// Batch delete
db.Where("age < ?", 18).Delete(&User{})
```

---

## 6. Preloading (Eager Loading)

```go
// Separate queries (works for all relation types)
db.Preload("Orders").Find(&users)

// With conditions
db.Preload("Orders", "state = ?", "paid").Find(&users)

// Custom ordering
db.Preload("Orders", func(db *gorm.DB) *gorm.DB {
    return db.Order("orders.amount DESC")
}).Find(&users)

// Nested preloading
db.Preload("Orders.OrderItems.Product").Find(&users)

// Preload all first-level associations
db.Preload(clause.Associations).Find(&users)

// Joins preloading (single query, for belongs-to / has-one)
db.Joins("Company").Find(&users)
```

---

## 7. Transactions

```go
// Callback-based (recommended)
db.Transaction(func(tx *gorm.DB) error {
    if err := tx.Create(&user).Error; err != nil {
        return err  // auto rollback
    }
    if err := tx.Create(&order).Error; err != nil {
        return err  // auto rollback
    }
    return nil  // auto commit
})

// Manual
tx := db.Begin()
defer func() {
    if r := recover(); r != nil {
        tx.Rollback()
    }
}()
tx.Create(&user)
tx.Commit()

// Nested transactions / savepoints
db.Transaction(func(tx *gorm.DB) error {
    tx.Create(&user1)
    tx.Transaction(func(tx2 *gorm.DB) error {
        tx2.Create(&user2)
        return errors.New("rollback inner only")
    })
    return nil  // user1 committed, user2 rolled back
})

// Performance: skip default transaction wrapping
db, err := gorm.Open(sqlite.Open("app.db"), &gorm.Config{
    SkipDefaultTransaction: true,  // ~30% performance boost
})
```

---

## 8. Hooks / Callbacks

Hook signatures: `func (m *Model) HookName(tx *gorm.DB) error`

**Create**: BeforeSave -> BeforeCreate -> [insert] -> AfterCreate -> AfterSave
**Update**: BeforeSave -> BeforeUpdate -> [update] -> AfterUpdate -> AfterSave
**Delete**: BeforeDelete -> [delete] -> AfterDelete
**Query**: [query] -> AfterFind

```go
func (u *User) BeforeCreate(tx *gorm.DB) error {
    u.UUID = uuid.New()
    return nil
}
```

Returning an error rolls back the transaction. Skip hooks with:
```go
db.Session(&gorm.Session{SkipHooks: true}).Create(&user)
```

---

## 9. Auto-Migration

```go
db.AutoMigrate(&User{}, &Product{}, &Order{})
```

**Does**: Create tables, add missing columns/indexes/foreign keys, alter column types (size, precision, nullability)
**Does NOT**: Delete unused columns (data safety)

Disable FK constraints during migration:
```go
db, err := gorm.Open(sqlite.Open("app.db"), &gorm.Config{
    DisableForeignKeyConstraintWhenMigrating: true,
})
```

---

## 10. Session / Context Usage

```go
// Attach context (for timeouts, cancellation)
db.WithContext(ctx).Find(&users)

// DryRun mode (generate SQL without executing)
stmt := db.Session(&gorm.Session{DryRun: true}).First(&user, 1).Statement

// Prepared statements (cache for performance)
db.Session(&gorm.Session{PrepareStmt: true})

// NewDB (reset conditions - important for reusable query builders)
tx := db.Session(&gorm.Session{NewDB: true})

// Batch size for bulk inserts
db.Session(&gorm.Session{CreateBatchSize: 1000}).Create(&users)
```

---

## 11. Best Practices for Service Layer Wrapping

Based on the patterns observed in GORM docs:

1. **Store `*gorm.DB` in a service/repository struct**, pass it via constructor:
   ```go
   type UserRepository struct {
       db *gorm.DB
   }
   func NewUserRepository(db *gorm.DB) *UserRepository {
       return &UserRepository{db: db}
   }
   ```

2. **Accept `context.Context` in all public methods** and use `db.WithContext(ctx)`:
   ```go
   func (r *UserRepository) FindByID(ctx context.Context, id uint) (*User, error) {
       var user User
       err := r.db.WithContext(ctx).First(&user, id).Error
       return &user, err
   }
   ```

3. **Use `map[string]interface{}` for updates** when zero values matter; use struct updates only when zero values should be skipped.

4. **Use `db.Transaction()`** callback style for multi-step operations.

5. **Use `Preload`** explicitly rather than relying on lazy loading (which GORM doesn't have).

6. **Check `result.Error`** on every operation; `gorm.ErrRecordNotFound` for missing records.

---

## 12. Testing Patterns (In-Memory SQLite)

```go
func setupTestDB(t *testing.T) *gorm.DB {
    db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
    if err != nil {
        t.Fatalf("failed to connect to test db: %v", err)
    }
    db.AutoMigrate(&User{}, &Order{})  // create schema
    return db
}

func TestCreateUser(t *testing.T) {
    db := setupTestDB(t)
    repo := NewUserRepository(db)

    user := &User{Name: "test"}
    err := repo.Create(context.Background(), user)
    assert.NoError(t, err)
    assert.NotZero(t, user.ID)
}
```

Key points:
- `file::memory:?cache=shared` gives a fresh in-memory DB per test (shared cache allows multiple connections to the same in-memory DB)
- Call `AutoMigrate` in test setup to create schema
- Each test function gets its own DB instance for isolation
- No cleanup needed -- memory is freed when connections close
- `SkipDefaultTransaction: true` in test config for speed
