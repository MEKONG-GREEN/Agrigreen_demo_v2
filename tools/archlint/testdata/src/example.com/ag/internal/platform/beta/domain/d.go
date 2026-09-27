package domain

import "time"

func Stamp() time.Time { return time.Now() } // want "R-12"

func Ok(now time.Time) time.Time { return now.Add(time.Hour) }
