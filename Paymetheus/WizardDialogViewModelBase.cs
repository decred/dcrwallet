// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

namespace Paymetheus
{
    class WizardDialogViewModelBase : ViewModelBase
    {
        public WizardDialogViewModelBase(ShellViewModel shell, WizardViewModelBase wizard)
            : base(shell)
        {
            _shell = shell;
            _wizard = wizard;
        }

        protected ShellViewModel _shell;
        protected WizardViewModelBase _wizard;
    }
}
