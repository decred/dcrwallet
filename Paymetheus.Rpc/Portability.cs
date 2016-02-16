// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using System.IO;

namespace Paymetheus.Rpc
{
    static class Portability
    {
        public static string LocalAppData(PlatformID platform, string processName)
        {
            if (processName == null)
                throw new ArgumentNullException(nameof(processName));
            if (processName.Length == 0)
                throw new ArgumentException(nameof(processName) + " may not have zero length");

            switch (platform)
            {
                case PlatformID.Win32NT:
                    return Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),
                        ToUpper(processName));
                case PlatformID.MacOSX:
                    return Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.Personal),
                        "Library", "Application Support", ToUpper(processName));
                case PlatformID.Unix:
                    return Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.Personal),
                        ToDotLower(processName));
                default:
                    throw new PlatformNotSupportedException($"PlatformID={platform}");
            }
        }

        private static string ToUpper(string value)
        {
            var firstChar = value[0];
            if (char.IsUpper(firstChar))
                return value;
            else
                return char.ToUpper(firstChar) + value.Substring(1);
        }

        private static string ToDotLower(string value)
        {
            var firstChar = value[0];
            return "." + char.ToLower(firstChar) + value.Substring(1);
        }
    }
}
