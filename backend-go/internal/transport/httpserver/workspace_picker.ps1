$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;

public static class WorkspacePicker {
    [ComImport, Guid("DC1C5A9C-E88A-4DDE-A5A1-60F82A20AEF7")]
    private class FileOpenDialog {}
    // IFileDialog members are ordered by their native COM vtable slots.
    [ComImport, Guid("42F85136-DB7E-439C-85F1-E4075D135FC8"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
    private interface IFileDialog {
        [PreserveSig] int Show(IntPtr owner);
        void SetFileTypes(); void SetFileTypeIndex(); void GetFileTypeIndex();
        void Advise(); void Unadvise();
        void SetOptions(uint options); void GetOptions(out uint options);
        void SetDefaultFolder(); void SetFolder(); void GetFolder(); void GetCurrentSelection();
        void SetFileName(); void GetFileName();
        void SetTitle([MarshalAs(UnmanagedType.LPWStr)] string title);
        void SetOkButtonLabel(); void SetFileNameLabel();
        void GetResult(out IShellItem item);
    }
    [ComImport, Guid("43826D1E-E718-42EE-BC55-A1E261C37BFE"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
    private interface IShellItem {
        void BindToHandler(); void GetParent();
        void GetDisplayName(uint format, [MarshalAs(UnmanagedType.LPWStr)] out string name);
    }
    [DllImport("user32.dll")] private static extern IntPtr GetForegroundWindow();
    public static string Pick() {
        var dialog = (IFileDialog)new FileOpenDialog();
        uint options;
        dialog.GetOptions(out options);
        dialog.SetOptions(options | 0x20u | 0x40u | 0x800u); // folders, filesystem, existing paths
        dialog.SetTitle("\u9009\u62e9\u5de5\u4f5c\u533a\u6587\u4ef6\u5939");
        int hr = dialog.Show(GetForegroundWindow());
        if (hr == unchecked((int)0x800704C7)) return null;
        Marshal.ThrowExceptionForHR(hr);
        IShellItem item;
        dialog.GetResult(out item);
        string path;
        item.GetDisplayName(0x80058000u, out path); // SIGDN_FILESYSPATH
        return path;
    }
}
"@
[Console]::WriteLine([WorkspacePicker]::Pick())
