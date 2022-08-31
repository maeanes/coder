package metricscache

import (
	"reflect"
	"testing"
	"time"

	"github.com/coder/coder/coderd/database"
)

func date(year, month, day int) time.Time {
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

func Test_fillEmptyDAUDays(t *testing.T) {
	t.Parallel()

	type args struct {
		rows []database.GetDAUsFromAgentStatsRow
	}
	tests := []struct {
		name string
		args args
		want []database.GetDAUsFromAgentStatsRow
	}{
		{"empty", args{}, nil},
		{"no holes", args{
			rows: []database.GetDAUsFromAgentStatsRow{
				{
					Date: date(2022, 01, 01),
					Daus: 1,
				},
				{
					Date: date(2022, 01, 02),
					Daus: 1,
				},
				{
					Date: date(2022, 01, 03),
					Daus: 1,
				},
			},
		}, []database.GetDAUsFromAgentStatsRow{
			{
				Date: date(2022, 01, 01),
				Daus: 1,
			},
			{
				Date: date(2022, 01, 02),
				Daus: 1,
			},
			{
				Date: date(2022, 01, 03),
				Daus: 1,
			},
		}},
		{"holes", args{
			rows: []database.GetDAUsFromAgentStatsRow{
				{
					Date: date(2022, 1, 1),
					Daus: 3,
				},
				{
					Date: date(2022, 1, 4),
					Daus: 1,
				},
				{
					Date: date(2022, 1, 7),
					Daus: 3,
				},
			},
		}, []database.GetDAUsFromAgentStatsRow{
			{
				Date: date(2022, 1, 1),
				Daus: 3,
			},
			{
				Date: date(2022, 1, 2),
				Daus: 0,
			},
			{
				Date: date(2022, 1, 3),
				Daus: 0,
			},
			{
				Date: date(2022, 1, 4),
				Daus: 1,
			},
			{
				Date: date(2022, 1, 5),
				Daus: 0,
			},
			{
				Date: date(2022, 1, 6),
				Daus: 0,
			},
			{
				Date: date(2022, 1, 7),
				Daus: 3,
			},
		}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := fillEmptyDAUDays(tt.args.rows); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("fillEmptyDAUDays() = %v, want %v", got, tt.want)
			}
		})
	}
}
