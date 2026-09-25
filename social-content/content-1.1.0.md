# Social Media Content for v1.1.0

## Discord

```
# VPSBox v1.1.0 — Labs and Live Diagnostics

This release adds reproducible multi-VPS migration labs, live server diagnostics, and a clearer update experience while making Multipass workflows more reliable.

🧪 New Features
- Create, resume, inspect, snapshot, reset, export, and safely destroy migration labs from validated manifests.
- Start with included EasyPanel smoke, compatibility, and full profiles built from synthetic test fixtures.
- Monitor system logs, network connections, routes, Docker containers, container output, and recent Docker activity from the desktop Logs tab.
- Check for VPSBox updates from the System screen and jump directly to a newer build.

⚡ Improvements
- Export wildcard domains, TLS certificate paths, and lab roles for easier Server Compass imports and sandbox access.

🛠 Bug Fixes
- Multipass checkpoint restores now work in non-interactive jobs.
- Completed stop and destroy actions no longer appear to fail when local-domain updates need elevated privileges.
- Destroy now removes leftover keys, certificates, cloud-init data, and checkpoint baselines.
- Multipass cloud-init scripts retain their executable permissions during provisioning.

Download VPSBox v1.1.0: https://github.com/stoicsoft/vpsbox-app/releases
```

---

## Reddit

**Title**: VPSBox v1.1.0 adds reproducible migration labs and live server diagnostics

Hi everyone — VPSBox v1.1.0 is ready. The main addition is a manifest-driven lab workflow for creating repeatable multi-VPS migration environments. You can validate a lab, apply or resume it, inspect its state, snapshot and reset every VM, export all connections for Server Compass, and destroy only the VMs owned by that exact run. The release includes synthetic EasyPanel smoke, compatibility, and full profiles for migration testing.

The desktop app also has a new Logs tab with searchable system journal entries, active network connections, routes, Docker status, container output, and recent Docker activity. A new System card lets you force an update check, understand failures, and open the latest download.

We also fixed non-interactive Multipass restores, false failure reports after successful stop or destroy actions, leftover sandbox artifacts, and cloud-init script permissions during provisioning. Connection exports now include wildcard domains, TLS paths, and lab roles.

Release and downloads: https://github.com/stoicsoft/vpsbox-app/releases

If you try the new labs or diagnostics view, I’d love to hear what works well and what you want to see next.

---

## LinkedIn

VPSBox v1.1.0 is now available, bringing reproducible migration testing and live diagnostics to local Ubuntu sandboxes.

Key highlights:

- Create and manage repeatable multi-VPS labs from validated manifests, with included synthetic EasyPanel test profiles.
- Inspect system, network, routing, and Docker diagnostics from a searchable desktop Logs tab.
- Check for updates from the System screen with clear status, error feedback, and a direct download action.
- Export wildcard domains, TLS certificate paths, and lab roles for smoother Server Compass handoff.
- Restore, stop, destroy, and provision Multipass sandboxes more reliably in non-interactive workflows.

Download VPSBox v1.1.0: https://github.com/stoicsoft/vpsbox-app/releases

#VPSBox #DevOps #SelfHosted #Docker #Virtualization

---

## X (Twitter)

VPSBox v1.1.0: reproducible multi-VPS migration labs, live system/network/Docker diagnostics, manual update checks, and more reliable Multipass restore and provisioning.

https://github.com/stoicsoft/vpsbox-app/releases

#VPSBox #DevOps #SelfHosted

---

## Facebook (Vietnamese)

# VPSBox v1.1.0 — Lab tái lập và chẩn đoán trực tiếp

Phiên bản này bổ sung quy trình dựng lab migration nhiều máy ảo có thể tái lập, màn hình chẩn đoán server trực tiếp và trải nghiệm kiểm tra cập nhật rõ ràng hơn. Các thao tác với Multipass cũng ổn định hơn khi chạy không tương tác.

🧪 Tính năng mới

- Tạo, tiếp tục, kiểm tra trạng thái, snapshot, reset, export và hủy lab an toàn từ manifest đã được kiểm tra chặt chẽ.
- Dùng ngay ba profile EasyPanel gồm smoke, compatibility và full với dữ liệu mô phỏng, phù hợp để thử nghiệm migration mà không phụ thuộc dữ liệu thật.
- Theo dõi system journal, kết nối mạng, bảng định tuyến, container, log container và hoạt động Docker gần đây ngay trong tab Logs; có tự động làm mới, lọc theo nhóm và tìm kiếm.
- Kiểm tra phiên bản VPSBox mới trong màn hình System, xem lỗi khi kiểm tra và mở trang tải bản mới nhất.

⚡ Cải tiến

- Khi export sang Server Compass hoặc shell, VPSBox bổ sung wildcard domain, đường dẫn chứng chỉ TLS và vai trò của từng máy trong lab để nhập và quản lý server dễ hơn.

🛠 Sửa lỗi

- Khôi phục checkpoint Multipass hoạt động đúng trong tiến trình không tương tác.
- Lệnh dừng hoặc hủy máy ảo đã hoàn tất sẽ không còn bị báo thất bại chỉ vì chưa có quyền cập nhật local domain.
- Khi hủy sandbox, VPSBox dọn luôn SSH key, chứng chỉ TLS, dữ liệu cloud-init và checkpoint baseline còn lại.
- Script cloud-init giữ đúng quyền thực thi khi Multipass gộp cấu hình provisioning.

Tải VPSBox v1.1.0: https://github.com/stoicsoft/vpsbox-app/releases

#VPSBox #DevOps #SelfHosted #Docker #QuanLyServer
