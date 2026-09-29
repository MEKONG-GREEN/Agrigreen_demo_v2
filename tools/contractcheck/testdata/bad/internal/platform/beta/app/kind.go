// Ca vi phạm: vùng nền tảng phát sự kiện vào vhost nội bộ MKG.
package app

import "example.com/ag/internal/shared/outbox"

func sneak() outbox.Event { return outbox.Event{Kind: outbox.KindInternal} }
