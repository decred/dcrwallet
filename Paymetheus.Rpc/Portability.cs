// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using System.IO;

namespace Paymetheus.Rpc
{
    public static class Portability
    {
        public static string LocalAppData(PlatformID platform, string organization, string product)
        {
            if (organization == null)
                throw new ArgumentNullException(nameof(organization));
            if (product == null)
                throw new ArgumentNullException(nameof(product));
            if (product.Length == 0)
                throw new ArgumentException(nameof(product) + " may not have zero length");

            switch (platform)
            {
                case PlatformID.Win32NT:
                    return Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),
                        ToUpper(organization), ToUpper(product));
                case PlatformID.MacOSX:
                    return Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.Personal),
                        "Library", "Application Support", ToUpper(organization), ToUpper(product));
                case PlatformID.Unix:
                    var homeDirectory = Environment.GetFolderPath(Environment.SpecialFolder.Personal);
                    if (organization == "")
                    {
                        return Path.Combine(homeDirectory, ToDotLower(product));
                    }
                    else
                    {
                        return Path.Combine(homeDirectory, ToDotLower(organization), product.ToLower());
                    }
                default:
                    throw PlatformNotSupported(platform);
            }
        }

        private static string ToUpper(string value)
        {
            if (value == "")
                return "";

            var firstChar = value[0];
            if (char.IsUpper(firstChar) || !char.IsLetter(firstChar))
                return value;
            else
                return char.ToUpper(firstChar) + value.Substring(1);
        }

        private static string ToDotLower(string value)
        {
            if (value == "")
                return "";

            return "." + value.ToLower();
        }

        public static string ExecutableInstallationPath(PlatformID platform, string organization, string executableName)
        {
            var folder = "Paymetheus";
            // XXX: hack for the launcher, dcrd is not in Paymetheus folder.
            if (executableName == "dcrd")
                folder = "Dcrd";

            // Unix and Mac: Unsure where the expected installation paths are or would be.
            // These are staying unimplemeted until it actually matters.
            switch (platform)
            {
                case PlatformID.Win32NT:
                    return Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.ProgramFiles),
                        ToUpper(organization), folder, executableName.ToLower() + ".exe");
                case PlatformID.Unix:
                    throw new NotImplementedException();
                case PlatformID.MacOSX:
                    throw new NotImplementedException();
                default:
                    throw PlatformNotSupported(platform);
            }
        }

        private static PlatformNotSupportedException PlatformNotSupported(PlatformID platform) =>
            new PlatformNotSupportedException($"PlatformID={platform}");
    }
}
