// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;

namespace Paymetheus.Rpc
{
    public struct SemanticVersion
    {
        public SemanticVersion(uint major, uint minor, uint patch)
        {
            Major = major;
            Minor = minor;
            Patch = patch;
        }

        public uint Major { get; }
        public uint Minor { get; }
        public uint Patch { get; }

        public override string ToString() => $"{Major}.{Minor}.{Patch}";

        public static void AssertCompatible(SemanticVersion required, SemanticVersion actual)
        {
            if (required.Major != actual.Major)
                throw IncompatibleVersionException.Major(required, actual);
            if (required.Minor > actual.Minor)
                throw IncompatibleVersionException.Minor(required, actual);
            if (required.Minor == actual.Minor && required.Patch > actual.Patch)
                throw IncompatibleVersionException.Patch(required, actual);
        }
    }

    public class IncompatibleVersionException : Exception
    {
        private IncompatibleVersionException(string whatVersion, SemanticVersion required, SemanticVersion actual)
            : base($"Incompatible {whatVersion} versions: required={required} actual={actual}")
        { }

        public static IncompatibleVersionException Major(SemanticVersion required, SemanticVersion actual) =>
            new IncompatibleVersionException("major", required, actual);

        public static IncompatibleVersionException Minor(SemanticVersion required, SemanticVersion actual) =>
            new IncompatibleVersionException("minor", required, actual);

        public static IncompatibleVersionException Patch(SemanticVersion required, SemanticVersion actual) =>
            new IncompatibleVersionException("patch", required, actual);
    }
}
