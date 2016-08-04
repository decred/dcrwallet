// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information

using Paymetheus.Framework;

namespace Launcher.ViewModels
{
    public class ViewModelLocator
    {
        public static object LauncherProgressViewModel => SingletonViewModelLocator.Resolve("LauncherProgressView");
    }
}
