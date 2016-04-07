// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System.Linq;
using System.Reflection;

namespace Paymetheus
{
    static class AssemblyResources
    {
        public static string Title { get; }
        public static string ProductName { get; }
        public static string Organization { get; }

        static AssemblyResources()
        {
            var asm = Assembly.GetEntryAssembly();
            var metadata = asm?.GetCustomAttributes<AssemblyMetadataAttribute>();

            AssemblyTitleAttribute titleAttribute = null;
            try
            {
                titleAttribute = asm?.GetCustomAttribute<AssemblyTitleAttribute>();
            }
            finally
            {
                Title = titleAttribute?.Title ?? "Paymetheus";
            }

            AssemblyProductAttribute productAttribute = null;
            try
            {
                productAttribute = asm?.GetCustomAttribute<AssemblyProductAttribute>();
            }
            finally
            {
                ProductName = productAttribute?.Product ?? "Paymetheus";
            }

            AssemblyMetadataAttribute organizationAttribute = null;
            try
            {
                organizationAttribute = metadata?.FirstOrDefault(a => a.Key == "Organization");
            }
            finally
            {
                Organization = organizationAttribute?.Value ?? "";
            }
        }
    }
}
