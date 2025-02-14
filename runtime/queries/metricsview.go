package queries

import (
	"context"
	"fmt"
	"strings"

	runtimev1 "github.com/rilldata/rill/proto/gen/rill/runtime/v1"
	"github.com/rilldata/rill/runtime"
	"github.com/rilldata/rill/runtime/drivers"
	"github.com/rilldata/rill/runtime/server/pbutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

func lookupMetricsView(ctx context.Context, rt *runtime.Runtime, instanceID, name string) (*runtimev1.MetricsView, error) {
	obj, err := rt.GetCatalogEntry(ctx, instanceID, name)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	if obj.GetMetricsView() == nil {
		return nil, status.Errorf(codes.NotFound, "object named '%s' is not a metrics view", name)
	}

	return obj.GetMetricsView(), nil
}

func metricsQuery(ctx context.Context, olap drivers.OLAPStore, priority int, sql string, args []any) ([]*runtimev1.MetricsViewColumn, []*structpb.Struct, error) {
	rows, err := olap.Execute(ctx, &drivers.Statement{
		Query:    sql,
		Args:     args,
		Priority: priority,
	})
	if err != nil {
		return nil, nil, status.Error(codes.InvalidArgument, err.Error())
	}
	defer rows.Close()

	data, err := rowsToData(rows)
	if err != nil {
		return nil, nil, status.Error(codes.Internal, err.Error())
	}

	return structTypeToMetricsViewColumn(rows.Schema), data, nil
}

func rowsToData(rows *drivers.Result) ([]*structpb.Struct, error) {
	var data []*structpb.Struct
	for rows.Next() {
		rowMap := make(map[string]any)
		err := rows.MapScan(rowMap)
		if err != nil {
			return nil, err
		}

		rowStruct, err := pbutil.ToStruct(rowMap)
		if err != nil {
			return nil, err
		}

		data = append(data, rowStruct)
	}

	err := rows.Err()
	if err != nil {
		return nil, err
	}

	return data, nil
}

func structTypeToMetricsViewColumn(v *runtimev1.StructType) []*runtimev1.MetricsViewColumn {
	res := make([]*runtimev1.MetricsViewColumn, len(v.Fields))
	for i, f := range v.Fields {
		res[i] = &runtimev1.MetricsViewColumn{
			Name:     f.Name,
			Type:     f.Type.Code.String(),
			Nullable: f.Type.Nullable,
		}
	}
	return res
}

// buildFilterClauseForMetricsViewFilter builds a SQL string of conditions joined with AND.
// Unless the result is empty, it is prefixed with "AND".
// I.e. it has the format "AND (...) AND (...) ...".
func buildFilterClauseForMetricsViewFilter(filter *runtimev1.MetricsViewFilter) (string, []any, error) {
	var clauses []string
	var args []any

	if filter != nil && filter.Include != nil {
		clause, clauseArgs, err := buildFilterClauseForConditions(filter.Include, false)
		if err != nil {
			return "", nil, err
		}
		clauses = append(clauses, clause)
		args = append(args, clauseArgs...)
	}

	if filter != nil && filter.Exclude != nil {
		clause, clauseArgs, err := buildFilterClauseForConditions(filter.Exclude, true)
		if err != nil {
			return "", nil, err
		}
		clauses = append(clauses, clause)
		args = append(args, clauseArgs...)
	}

	return strings.Join(clauses, " "), args, nil
}

// buildFilterClauseForConditions returns a string with the format "AND (...) AND (...) ..."
func buildFilterClauseForConditions(conds []*runtimev1.MetricsViewFilter_Cond, exclude bool) (string, []any, error) {
	var clauses []string
	var args []any

	for _, cond := range conds {
		condClause, condArgs, err := buildFilterClauseForCondition(cond, exclude)
		if err != nil {
			return "", nil, err
		}
		if condClause == "" {
			continue
		}
		clauses = append(clauses, condClause)
		args = append(args, condArgs...)
	}

	return strings.Join(clauses, " "), args, nil
}

// buildFilterClauseForCondition returns a string with the format "AND (...)"
func buildFilterClauseForCondition(cond *runtimev1.MetricsViewFilter_Cond, exclude bool) (string, []any, error) {
	var clauses []string
	var args []any

	name := safeName(cond.Name)
	notKeyword := ""
	if exclude {
		notKeyword = "NOT"
	}

	// Tracks if we found NULL(s) in cond.In
	inHasNull := false

	// Build "dim [NOT] IN (?, ?, ...)" clause
	if len(cond.In) > 0 {
		// Add to args, skipping nulls
		for _, val := range cond.In {
			if _, ok := val.Kind.(*structpb.Value_NullValue); ok {
				inHasNull = true
				continue // Handled later using "dim IS [NOT] NULL" clause
			}
			arg, err := pbutil.FromValue(val)
			if err != nil {
				return "", nil, fmt.Errorf("filter error: %w", err)
			}
			args = append(args, arg)
		}

		// If there were non-null args, add a "dim [NOT] IN (...)" clause
		if len(args) > 0 {
			questionMarks := strings.Join(repeatString("?", len(args)), ",")
			clause := fmt.Sprintf("%s %s IN (%s)", name, notKeyword, questionMarks)
			clauses = append(clauses, clause)
		}
	}

	// Build "dim [NOT] ILIKE ?"
	if len(cond.Like) > 0 {
		for _, val := range cond.Like {
			// Add arg
			args = append(args, val)

			// Add clause
			clause := fmt.Sprintf("%s %s ILIKE ?", name, notKeyword)
			clauses = append(clauses, clause)
		}
	}

	// Add null check
	// NOTE: DuckDB doesn't handle NULL values in an "IN" expression. They must be checked with a "dim IS [NOT] NULL" clause.
	if inHasNull {
		clauses = append(clauses, fmt.Sprintf("%s IS %s NULL", name, notKeyword))
	}

	// If no checks were added, exit
	if len(clauses) == 0 {
		return "", nil, nil
	}

	// Join conditions
	var condJoiner string
	if exclude {
		condJoiner = " AND "
	} else {
		condJoiner = " OR "
	}
	condsClause := strings.Join(clauses, condJoiner)

	// When you have "dim NOT IN (a, b, ...)", then NULL values are always excluded, even if NULL is not in the list.
	// E.g. this returns zero rows: "select * from (select 1 as a union select null as a) where a not in (1)"
	// We need to explicitly include it.
	if exclude && !inHasNull && len(condsClause) > 0 {
		condsClause += fmt.Sprintf(" OR %s IS NULL", name)
	}

	// Done
	return fmt.Sprintf("AND (%s) ", condsClause), args, nil
}

func repeatString(val string, n int) []string {
	res := make([]string, n)
	for i := 0; i < n; i++ {
		res[i] = val
	}
	return res
}
