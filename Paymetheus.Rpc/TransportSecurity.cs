// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using System.Diagnostics;
using System.IO;
using System.Text;
using System.Threading.Tasks;

namespace Paymetheus.Rpc
{
    public static class TransportSecurity
    {
        private static readonly string CertificatePath =
            Path.Combine(WalletProcess.ApplicationDataDirectory, "rpc.cert");

        public static async Task EnsureCertificatePairExists()
        {
            if (!File.Exists(CertificatePath))
            {
                await GenerateCertificatePair();
            }
        }

        private static async Task GenerateCertificatePair()
        {
            Directory.CreateDirectory(WalletProcess.ApplicationDataDirectory);

            var processInfo = new ProcessStartInfo();
            processInfo.FileName = "gencerts";
            processInfo.Arguments = "-d " + WalletProcess.ApplicationDataDirectory;
            processInfo.UseShellExecute = false;
            processInfo.CreateNoWindow = true;

            var exitCode = await Process.Start(processInfo).WaitForExitAsync();
            if (exitCode != 0 || !File.Exists(CertificatePath))
            {
                throw new Exception("Failed to generate RPC certificate pair");
            }
        }

        public static async Task<string> ReadCertificate()
        {
            using (var r = new StreamReader(CertificatePath, Encoding.UTF8))
            {
                return await r.ReadToEndAsync();
            }
        }
    }
}
